package converter

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
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
	MaxFrames  int     // Limit frame count (0 = all frames)
	Lossless   bool    // True: retain full original FPS & lossless encoding
	EdgePath   string  // Custom path to Edge or Chrome (optional; auto-detected if empty)
	StaticDir  string  // Optional custom directory containing static assets
	AppVersion string  // Tag 31 app name
}

// DefaultRenderOptions returns recommended settings.
func DefaultRenderOptions() RenderOptions {
	return RenderOptions{
		Quality:    0.80, // Calibrated sweet spot for visual excellence and optimal size
		FPS:        0,    // 0: smart auto (resample >30fps to 30fps)
		MaxFrames:  0,    // no limit
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

	if opts.Lossless {
		opts.Quality = 1.0
	} else if opts.Quality <= 0 || opts.Quality > 1.0 {
		opts.Quality = 0.80
	}

	sourceFPS := anim.FrameRate
	if sourceFPS <= 0 {
		sourceFPS = 30.0
	}
	sourceDurationFrames := float64(anim.OutPoint - anim.InPoint)
	if sourceDurationFrames <= 0 {
		sourceDurationFrames = 1.0
	}
	durationSeconds := sourceDurationFrames / float64(sourceFPS)

	// Target FPS calculation:
	targetFPS := sourceFPS
	if opts.Lossless {
		// Strict Lossless mode: preserve exact source FPS without resampling
		targetFPS = sourceFPS
	} else if opts.FPS > 0 {
		targetFPS = opts.FPS
	} else if targetFPS > 30.0 {
		targetFPS = 30.0
	}

	targetFrames := int(math.Round(durationSeconds * float64(targetFPS)))
	if targetFrames <= 0 {
		targetFrames = 1
	}
	if opts.MaxFrames > 0 && targetFrames > opts.MaxFrames {
		targetFrames = opts.MaxFrames
	}

	// 1. Bake frames via headless Edge with Alpha Cleaning and Deduplication
	frames, frameIsDup, err := bakeLottieFrames(anim, targetFrames, targetFPS, opts)
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

	// 4. Emit unique Tag 49 (ImageBytesV3) records
	for idx, imgData := range uniqueImages {
		imageID := uint32(idx + 1)
		tag49, err := pag.BuildImageBytesV3(imageID, imgData, anim.Width, anim.Height, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building Tag 49 for image %d: %w", imageID, err)
		}
		container.AddTag(pag.TagImageBytesV3, tag49)
	}

	// 5. Emit optimized Layers with precise startTime and extended duration
	layers := make([][]byte, 0, len(layersTimings))
	for idx, lt := range layersTimings {
		layerID := uint32(100 + idx)
		layerPayload, err := pag.BuildImageLayerWithTime(layerID, lt.imageID, lt.name, lt.startTime, lt.duration, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building layer %d: %w", idx, err)
		}
		layers = append(layers, layerPayload)
	}

	// Tag 94: Performance/Markers tag (02 02 02)
	container.AddTag(pag.TagLayerMarkers, []byte{0x02, 0x02, 0x02})

	// Tag 2: VectorComposition containing all layers (CompID = 0)
	compPayload, err := pag.BuildVectorComposition(0, anim.Width, anim.Height, uint64(targetFrames), targetFPS, layers)
	if err != nil {
		return nil, fmt.Errorf("failed building VectorComposition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}

// bakeLottieFrames launches a local HTTP server and headless Edge to extract WebP canvas frames.
func bakeLottieFrames(anim *lottie.Animation, totalFrames int, fps float32, opts RenderOptions) ([][]byte, []bool, error) {
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

	mux := http.NewServeMux()

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
    async function start() {
        const resp = await fetch('/data.json');
        const animData = await resp.json();
        const container = document.getElementById('anim');
        
        const anim = lottie.loadAnimation({
            container: container,
            renderer: 'canvas',
            loop: false,
            autoplay: false,
            animationData: animData
        });

        anim.addEventListener('DOMLoaded', async () => {
            const canvas = container.querySelector('canvas');
            const ctx = canvas.getContext('2d');
            const totalFrames = %d;
            const quality = %.2f;
            const sourceInPoint = %.2f;
            const sourceDuration = %.2f;
            let prevData = null;

            for (let i = 0; i < totalFrames; i++) {
                const lottieFrame = sourceInPoint + (i / totalFrames) * sourceDuration;
                anim.goToAndStop(lottieFrame, true);
                await new Promise(r => requestAnimationFrame(r));
                
                const imgData = ctx.getImageData(0, 0, canvas.width, canvas.height);
                const d = imgData.data;

                // 1. Alpha Cleaning: zero out RGB values of transparent pixels to maximize WebP compression
                let dirty = false;
                for (let p = 0; p < d.length; p += 4) {
                    if (d[p + 3] === 0) {
                        if (d[p] !== 0 || d[p+1] !== 0 || d[p+2] !== 0) {
                            d[p] = 0; d[p+1] = 0; d[p+2] = 0;
                            dirty = true;
                        }
                    }
                }
                if (dirty) {
                    ctx.putImageData(imgData, 0, 0);
                }

                // 2. Perceptual Deduplication: detect visually identical consecutive frames
                let isDup = false;
                if (prevData && prevData.length === d.length) {
                    let diff = 0;
                    for (let p = 0; p < d.length; p += 4) {
                        diff += Math.abs(d[p] - prevData[p]) +
                                Math.abs(d[p+1] - prevData[p+1]) +
                                Math.abs(d[p+2] - prevData[p+2]) +
                                Math.abs(d[p+3] - prevData[p+3]);
                        if (diff > 50) { isDup = false; break; }
                    }
                    if (diff <= 50) isDup = true;
                }

                if (isDup) {
                    await fetch('/frame?idx=' + i + '&dup=1', { method: 'POST' });
                } else {
                    prevData = new Uint8ClampedArray(d);
                    const dataUrl = canvas.toDataURL('image/webp', quality);
                    const b64 = dataUrl.split(',')[1];
                    
                    await fetch('/frame?idx=' + i, {
                        method: 'POST',
                        headers: {'Content-Type': 'text/plain'},
                        body: b64
                    });
                }
            }

            await fetch('/done', { method: 'POST' });
        });
    }
    start();
    </script>
</body>
</html>`, anim.Width, anim.Height, totalFrames, opts.Quality, sourceInPoint, sourceDuration)
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

		mu.Lock()
		frames[idx] = rawBytes
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
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, nil, errors.New("timeout waiting for frame baking")
	}

	cmd.Process.Kill()

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
