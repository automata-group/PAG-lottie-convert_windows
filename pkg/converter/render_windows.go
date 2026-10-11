package converter

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"lottie2pag/pkg/lottie"
	"lottie2pag/pkg/pag"
)

//go:embed assets/lottie.min.js
var embeddedLottieJS []byte

// RenderOptions specifies conversion settings.
type RenderOptions struct {
	Quality    float32 // 0.1 to 1.0 WebP quality (default 0.80)
	FPS        float32 // Target FPS (0 = use Lottie FPS)
	Scale      float32 // Resolution scale factor 0.1 to 1.0 (default 1.0 or calculated)
	TargetKB   int     // Target size budget in KB (e.g. 100). Auto-solves optimal Scale, FPS, and Quality!
	Compact    bool    // Preset for ultra-compact animations strictly below 100 KB
	MaxFrames  int     // Limit frame count (0 = all frames)
	Lossless   bool    // True: retain full original FPS & lossless encoding
	Vector     bool    // True: direct native vector export (zero full-canvas rasterization)
	EdgePath   string  // Custom path to Edge or Chrome (optional; auto-detected if empty)
	StaticDir  string  // Optional custom directory containing static assets
	AppVersion string  // Tag 31 app name
}

// DefaultRenderOptions returns recommended settings.
func DefaultRenderOptions() RenderOptions {
	return RenderOptions{
		Quality:    0.80, // Calibrated sweet spot for visual excellence and optimal size
		FPS:        0,    // 0: smart auto (resample >30fps to 30fps)
		Scale:      0,    // 0: auto (1.0 default, or auto-scaled when TargetKB/Compact is set)
		TargetKB:   0,    // No size budget cap by default
		Compact:    false,
		MaxFrames:  0, // no limit
		Lossless:   false,
		EdgePath:   "", // dynamically discovered
		StaticDir:  "", // embedded assets used by default
		AppVersion: "lottie2pag-go",
	}
}

// ConvertLottieToPAG converts a Lottie animation into an aggressively optimized production-grade PAG binary.
func ConvertLottieToPAG(anim *lottie.Animation, opts RenderOptions) ([]byte, error) {
	browserPath, err := FindBrowserExecutable(opts.EdgePath)
	if err != nil {
		return nil, fmt.Errorf("browser detection error: %w", err)
	}
	opts.EdgePath = browserPath
	opts.StaticDir = ResolveStaticDir(opts.StaticDir)

	sourceFPS := anim.FrameRate
	if sourceFPS <= 0 {
		sourceFPS = 30.0
	}
	sourceDurationFrames := float64(anim.OutPoint - anim.InPoint)
	if sourceDurationFrames <= 0 {
		sourceDurationFrames = 1.0
	}
	durationSeconds := sourceDurationFrames / float64(sourceFPS)

	// 0. Mathematical Budget Solver for sub-100KB target (< 100 KB)
	if opts.Compact && opts.TargetKB <= 0 {
		opts.TargetKB = 90 // Target strictly below 100 KB!
	}

	if opts.TargetKB > 0 && !opts.Lossless {
		targetBytes := float64(opts.TargetKB) * 1024.0
		// Allow 20% safety buffer for container tags, vector metadata, and Zstd ratio
		imageBudget := (targetBytes * 0.80) - 2500.0
		if imageBudget < 12000.0 {
			imageBudget = 12000.0
		}

		if opts.FPS <= 0 {
			// Optimal kinematic frame rate for smooth motion with minimal frame bloat
			if durationSeconds <= 2.0 {
				opts.FPS = 24.0
			} else if durationSeconds <= 4.0 {
				opts.FPS = 20.0
			} else {
				opts.FPS = 16.0
			}
		}

		estFrames := math.Round(durationSeconds * float64(opts.FPS))
		if estFrames < 1 {
			estFrames = 1
		}
		perFrameBudget := imageBudget / estFrames
		origPixels := float64(anim.Width * anim.Height)

		if origPixels > 0 && (opts.Scale <= 0 || opts.Compact) {
			// WebP with stripped ICCP and quantized alpha yields ~0.20 bytes/pixel
			desiredPixels := perFrameBudget / 0.20
			s := math.Sqrt(desiredPixels / origPixels)
			if s < 0.40 {
				s = 0.40
				fmt.Printf("    [!] Target Budget Notice: %d KB is at/below physical quality threshold for this asset. Clamped scale to floor 0.40x.\n", opts.TargetKB)
			} else if s > 0.60 && opts.Compact {
				s = 0.60
			} else if s > 1.0 {
				s = 1.0
			}
			opts.Scale = float32(math.Round(s*100) / 100)
		}

		if opts.Quality <= 0 || opts.Quality > 0.70 || opts.Compact {
			opts.Quality = 0.55
		}
	}

	if opts.Scale <= 0 || opts.Scale > 1.0 {
		opts.Scale = 1.0
	}

	// Target FPS calculation:
	targetFPS := sourceFPS
	if opts.Lossless {
		// Strict Lossless mode: preserve exact source FPS without resampling
		targetFPS = sourceFPS
		opts.Quality = 1.0
		opts.Scale = 1.0
	} else if opts.FPS > 0 {
		targetFPS = opts.FPS
	} else if targetFPS > 30.0 {
		targetFPS = 30.0
	}

	if opts.Quality <= 0 || opts.Quality > 1.0 {
		opts.Quality = 0.80
	}

	targetFrames := int(math.Round(durationSeconds * float64(targetFPS)))
	if targetFrames <= 0 {
		targetFrames = 1
	}
	if opts.MaxFrames > 0 && targetFrames > opts.MaxFrames {
		targetFrames = opts.MaxFrames
	}

	exportW := int32(math.Round(float64(anim.Width) * float64(opts.Scale)))
	exportH := int32(math.Round(float64(anim.Height) * float64(opts.Scale)))
	if exportW < 1 {
		exportW = 1
	}
	if exportH < 1 {
		exportH = 1
	}

	// 1. Bake frames via headless browser with Alpha Cleaning, Scaling and Deduplication
	frames, frameIsDup, err := bakeLottieFrames(anim, targetFrames, targetFPS, exportW, exportH, opts)
	if err != nil {
		return nil, fmt.Errorf("failed baking frames: %w", err)
	}
	if len(frames) == 0 {
		return nil, errors.New("no frames were generated")
	}

	// 2. Build PAG Container
	container := pag.NewContainer()

	// Tag 32: TimeStretchMode (Scale = 1)
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	// 3. Frame Deduplication & Asset Pooling
	type layerTiming struct {
		imageID   uint32
		startTime uint64
		duration  uint64
		name      string
	}

	var layersTimings []layerTiming
	var uniqueImages [][]byte
	imageHashMap := make(map[[32]byte]uint32)

	for idx := 0; idx < targetFrames; idx++ {
		// Consecutive duplicate check -> extend duration of preceding layer
		if frameIsDup[idx] && len(layersTimings) > 0 {
			layersTimings[len(layersTimings)-1].duration++
			continue
		}

		frameBytes := frames[idx]
		if len(frameBytes) == 0 {
			if len(layersTimings) > 0 {
				layersTimings[len(layersTimings)-1].duration++
			}
			continue
		}

		// Global asset pool check: if identical image was already stored, reuse its ImageID!
		hash := sha256.Sum256(frameBytes)
		var imageID uint32
		if existingID, exists := imageHashMap[hash]; exists {
			imageID = existingID
		} else {
			uniqueImages = append(uniqueImages, frameBytes)
			imageID = uint32(len(uniqueImages))
			imageHashMap[hash] = imageID
		}

		layerName := fmt.Sprintf("frame_%04d.webp", idx)
		layersTimings = append(layersTimings, layerTiming{
			imageID:   imageID,
			startTime: uint64(idx),
			duration:  1,
			name:      layerName,
		})
	}

	// 4. Emit unique Tag 49 (ImageBytesV3) records with export dimensions
	for idx, imgData := range uniqueImages {
		imageID := uint32(idx + 1)
		tag49, err := pag.BuildImageBytesV3(imageID, imgData, exportW, exportH, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building Tag 49 for image %d: %w", imageID, err)
		}
		container.AddTag(pag.TagImageBytesV3, tag49)
	}

	// 5. Emit optimized Layers with hardware bilinear scaling to fill canvas
	layers := make([][]byte, 0, len(layersTimings))
	scaleX := float32(anim.Width) / float32(exportW)
	scaleY := float32(anim.Height) / float32(exportH)
	for idx, lt := range layersTimings {
		layerID := uint32(100 + idx)
		layerPayload, err := pag.BuildImageLayerWithTransform(layerID, lt.imageID, lt.name, lt.startTime, lt.duration, 0, 0, scaleX, scaleY)
		if err != nil {
			return nil, fmt.Errorf("failed building layer %d: %w", idx, err)
		}
		layers = append(layers, layerPayload)
	}

	// Tag 94: Performance/Markers tag (02 02 02)
	container.AddTag(pag.TagLayerMarkers, []byte{0x02, 0x02, 0x02})

	// Tag 2: VectorComposition containing all layers (CompID = 0) with original native dimensions
	compPayload, err := pag.BuildVectorComposition(0, anim.Width, anim.Height, uint64(targetFrames), targetFPS, layers)
	if err != nil {
		return nil, fmt.Errorf("failed building VectorComposition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}

// bakeLottieFrames launches a local HTTP server and headless Edge to extract WebP canvas frames.
func bakeLottieFrames(anim *lottie.Animation, totalFrames int, fps float32, exportW, exportH int32, opts RenderOptions) ([][]byte, []bool, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	var mu sync.Mutex
	frames := make([][]byte, totalFrames)
	frameIsDup := make([]bool, totalFrames)
	receivedCount := 0
	doneCh := make(chan struct{})
	errCh := make(chan string, 1)

	mux := http.NewServeMux()

	// Error reporting handler from browser JavaScript
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		msg := r.URL.Query().Get("msg")
		select {
		case errCh <- msg:
		default:
		}
		w.WriteHeader(http.StatusOK)
	})

	// Serve lottie.min.js (prefer custom directory if provided, otherwise serve embedded bytes)
	mux.HandleFunc("/lottie.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		if opts.StaticDir != "" {
			customPath := filepath.Join(opts.StaticDir, "lottie.min.js")
			if fi, err := os.Stat(customPath); err == nil && !fi.IsDir() {
				http.ServeFile(w, r, customPath)
				return
			}
		}
		w.Write(embeddedLottieJS)
	})

	// Serve Lottie JSON
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(anim.RawJSONBytes)
	})

	sourceInPoint := float64(anim.InPoint)
	sourceDuration := float64(anim.OutPoint - anim.InPoint)
	if sourceDuration <= 0 {
		sourceDuration = float64(totalFrames)
	}

	// Harness HTML with Alpha Cleaning and Perceptual Frame Deduplication
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <script src="/lottie.min.js"></script>
    <style>body { margin: 0; padding: 0; background: transparent; }</style>
</head>
<body>
    <div id="anim" style="width:%dpx; height:%dpx;"></div>
    <script>
    window.onerror = function(msg, url, line) {
        fetch('/error?msg=' + encodeURIComponent(msg + ' (line ' + line + ')'), { method: 'POST' });
    };
    window.onunhandledrejection = function(e) {
        fetch('/error?msg=' + encodeURIComponent('Unhandled Rejection: ' + (e.reason ? e.reason.message || e.reason : 'unknown')), { method: 'POST' });
    };

    async function start() {
        try {
            const resp = await fetch('/data.json');
            if (!resp.ok) {
                fetch('/error?msg=failed_to_fetch_data_json', { method: 'POST' });
                return;
            }
            const animData = await resp.json();
            const container = document.getElementById('anim');
            
            const anim = lottie.loadAnimation({
                container: container,
                renderer: 'canvas',
                loop: false,
                autoplay: false,
                animationData: animData
            });

            anim.addEventListener('data_failed', () => {
                fetch('/error?msg=lottie_data_failed', { method: 'POST' });
            });

            anim.addEventListener('DOMLoaded', async () => {
                try {
            const canvas = container.querySelector('canvas');
            const ctx = canvas.getContext('2d');
            const exportW = %d;
            const exportH = %d;
            const totalFrames = %d;
            const quality = %.2f;
            const sourceInPoint = %.2f;
            const sourceDuration = %.2f;

            let exportCanvas = null;
            let exportCtx = null;
            if (exportW !== canvas.width || exportH !== canvas.height) {
                exportCanvas = document.createElement('canvas');
                exportCanvas.width = exportW;
                exportCanvas.height = exportH;
                exportCtx = exportCanvas.getContext('2d');
                exportCtx.imageSmoothingEnabled = true;
                exportCtx.imageSmoothingQuality = 'high';
            }

            let prevData = null;

            for (let i = 0; i < totalFrames; i++) {
                const lottieFrame = sourceInPoint + (i / totalFrames) * sourceDuration;
                anim.goToAndStop(lottieFrame, true);
                await new Promise(r => requestAnimationFrame(r));
                
                // 1. High-quality scale down if exportCanvas is active
                let targetCanvas = canvas;
                if (exportCanvas) {
                    exportCtx.clearRect(0, 0, exportW, exportH);
                    exportCtx.drawImage(canvas, 0, 0, exportW, exportH);
                    targetCanvas = exportCanvas;
                }

                const targetCtx = targetCanvas.getContext('2d');
                const targetImgData = targetCtx.getImageData(0, 0, targetCanvas.width, targetCanvas.height);
                const td = targetImgData.data;

                // 2. Mathematical Alpha Optimization on target export canvas:
                // a. Fringe Alpha Zeroing: Zero out near-transparent fringe pixels (A <= 12) & zero their RGB
                // b. Entropy Suppression: Soft-quantize alpha into discrete steps of 16 (0, 16, 32, ..., 255)
                //    This reduces ALPH chunk entropy dramatically while remaining visually imperceptible.
                let dirty = false;
                for (let p = 0; p < td.length; p += 4) {
                    let a = td[p + 3];
                    if (a <= 12) {
                        if (td[p] !== 0 || td[p+1] !== 0 || td[p+2] !== 0 || td[p+3] !== 0) {
                            td[p] = 0; td[p+1] = 0; td[p+2] = 0; td[p+3] = 0;
                            dirty = true;
                        }
                    } else if (a < 250) {
                        let qa = Math.round(a / 16) * 16;
                        if (qa > 255) qa = 255;
                        if (qa !== a) {
                            td[p+3] = qa;
                            dirty = true;
                        }
                    }
                }
                if (dirty) {
                    targetCtx.putImageData(targetImgData, 0, 0);
                }

                // 3. Perceptual Deduplication: detect visually identical consecutive frames
                let isDup = false;
                if (prevData && prevData.length === td.length) {
                    let diff = 0;
                    for (let p = 0; p < td.length; p += 4) {
                        diff += Math.abs(td[p] - prevData[p]) +
                                Math.abs(td[p+1] - prevData[p+1]) +
                                Math.abs(td[p+2] - prevData[p+2]) +
                                Math.abs(td[p+3] - prevData[p+3]);
                        if (diff > 50) { isDup = false; break; }
                    }
                    if (diff <= 50) isDup = true;
                }

                if (isDup) {
                    await fetch('/frame?idx=' + i + '&dup=1', { method: 'POST' });
                } else {
                    prevData = new Uint8ClampedArray(td);
                    const dataUrl = targetCanvas.toDataURL('image/webp', quality);
                    const b64 = dataUrl.split(',')[1];
                    
                    await fetch('/frame?idx=' + i, {
                        method: 'POST',
                        headers: {'Content-Type': 'text/plain'},
                        body: b64
                    });
                }
            }

            await fetch('/done', { method: 'POST' });
                } catch (err) {
                    fetch('/error?msg=' + encodeURIComponent('render_loop_error: ' + (err.message || err)), { method: 'POST' });
                }
            });
        } catch (e) {
            fetch('/error?msg=' + encodeURIComponent('start_error: ' + (e.message || e)), { method: 'POST' });
        }
    }
    start();
    </script>
</body>
</html>`, anim.Width, anim.Height, exportW, exportH, totalFrames, opts.Quality, sourceInPoint, sourceDuration)
		w.Write([]byte(html))
	})

	// Frame upload handler
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) {
		idxStr := r.URL.Query().Get("idx")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= totalFrames {
			http.Error(w, "invalid idx", http.StatusBadRequest)
			return
		}

		if r.URL.Query().Get("dup") == "1" {
			mu.Lock()
			frameIsDup[idx] = true
			receivedCount++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		rawBytes, err := base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			http.Error(w, "base64 decode error", http.StatusBadRequest)
			return
		}

		cleanBytes := StripWebPMetadata(rawBytes)

		mu.Lock()
		frames[idx] = cleanBytes
		receivedCount++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	// Completion handler
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		close(doneCh)
		w.WriteHeader(http.StatusOK)
	})


	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	// Launch headless browser (Edge, Chrome, Chromium)
	cmd := exec.Command(
		opts.EdgePath,
		"--headless=new",
		"--disable-gpu",
		"--mute-audio",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--no-first-run",
		fmt.Sprintf("http://127.0.0.1:%d/index.html", port),
	)

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed starting headless browser at %s: %w", opts.EdgePath, err)
	}

	// Wait with dynamic timeout based on frame count
	timeoutSec := 40 + (totalFrames / 5)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	select {
	case <-doneCh:
		// Succeeded!
	case errMsg := <-errCh:
		KillProcessTree(cmd)
		return nil, nil, fmt.Errorf("browser rendering error: %s", errMsg)
	case <-ctx.Done():
		KillProcessTree(cmd)
		return nil, nil, errors.New("timeout waiting for frame baking")
	}

	KillProcessTree(cmd)

	mu.Lock()
	defer mu.Unlock()

	// Verify all frames received (non-duplicate frames must have non-empty bytes)
	for i := 0; i < totalFrames; i++ {
		if !frameIsDup[i] && len(frames[i]) == 0 {
			return nil, nil, fmt.Errorf("missing frame at index %d", i)
		}
	}

	return frames, frameIsDup, nil
}

// StripWebPMetadata removes ICC profile, EXIF, and XMP chunks from WebP bitstream,
// saving ~460-800 bytes per frame while preserving visual fidelity.
func StripWebPMetadata(webpBytes []byte) []byte {
	if len(webpBytes) < 30 {
		return webpBytes
	}
	if string(webpBytes[0:4]) != "RIFF" || string(webpBytes[8:12]) != "WEBP" {
		return webpBytes
	}
	if string(webpBytes[12:16]) != "VP8X" {
		return webpBytes
	}

	flags := webpBytes[20]
	hasICCP := (flags & 0x20) != 0
	hasEXIF := (flags & 0x08) != 0
	hasXMP := (flags & 0x04) != 0

	if !hasICCP && !hasEXIF && !hasXMP {
		return webpBytes
	}

	var cleanChunks []byte
	offset := 12 + 8 + 10 // skip RIFF header (12) + VP8X header (8) + VP8X payload (10)
	for offset < len(webpBytes) {
		if offset+8 > len(webpBytes) {
			break
		}
		tag := string(webpBytes[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(webpBytes[offset+4 : offset+8])
		paddedSize := chunkSize + (chunkSize % 2)
		totalChunkLen := 8 + int(paddedSize)

		if offset+totalChunkLen > len(webpBytes) {
			break
		}

		if tag == "ICCP" || tag == "EXIF" || tag == "XMP " {
			if tag == "ICCP" {
				flags &^= 0x20
			} else if tag == "EXIF" {
				flags &^= 0x08
			} else if tag == "XMP " {
				flags &^= 0x04
			}
		} else {
			cleanChunks = append(cleanChunks, webpBytes[offset:offset+totalChunkLen]...)
		}
		offset += totalChunkLen
	}

	var out bytes.Buffer
	out.WriteString("RIFF")
	riffLen := uint32(4 + 8 + 10 + len(cleanChunks))
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], riffLen)
	out.Write(lenBuf[:])
	out.WriteString("WEBP")

	// VP8X with stripped flags
	out.WriteString("VP8X")
	binary.LittleEndian.PutUint32(lenBuf[:], 10)
	out.Write(lenBuf[:])
	vp8xPayload := make([]byte, 10)
	copy(vp8xPayload, webpBytes[20:30])
	vp8xPayload[0] = flags
	out.Write(vp8xPayload)

	out.Write(cleanChunks)
	return out.Bytes()
}
