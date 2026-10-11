package converter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"lottie2pag/pkg/lottie"
	"lottie2pag/pkg/pag"
)

// LayerCapabilityReport provides diagnostic audit details for a Lottie layer.
type LayerCapabilityReport struct {
	LayerIndex       int
	LayerName        string
	LayerType        int
	IsVectorEligible bool
	Reason           string
}

// ShouldFallbackLayer audits a Lottie layer to determine whether it can be exported
// as a 100% pure native vector tag or requires selective isolated rasterization fallback.
func ShouldFallbackLayer(l *lottie.Layer) (bool, string) {
	if l == nil {
		return false, ""
	}

	// 1. After Effects Filters & Effects (ef array, e.g. Gaussian Blur, Drop Shadow, Glow)
	if l.HasEffects() {
		return true, "contains unsupported After Effects filters/effects (ef)"
	}

	// 2. Complex Inverted or Luma Track Mattes (tt >= 2 or complex masks)
	if l.MatteType >= 2 {
		return true, "contains inverted or luma track matte (tt >= 2)"
	}

	// 3. Layer Type specific validation
	switch l.Type {
	case 1: // Solid Layer: native PAG SolidLayer
		return false, ""
	case 2: // Image Layer: native PAG ImageLayer with Tag 49 WebP
		return false, ""
	case 4: // Vector Shape Layer: check shape primitives
		elements := parseLottieShapeElements(l.Shapes)
		if len(elements) == 0 && len(l.Shapes) > 0 {
			// Shapes array present but contains unsupported operators (e.g. repeaters, custom trim, wiggles)
			return true, "contains unsupported vector shape operators or expressions"
		}
		return false, ""
	case 5: // Text Layer: text without pre-baked vector glyphs
		return true, "text layer requiring font rasterization fallback"
	case 0: // Precompose Layer
		return true, "nested precompose layer requiring isolated baking"
	case 3: // Null Layer
		return false, ""
	default:
		return false, ""
	}
}

// AuditAnimation performs a complete diagnostic capability audit across all layers in an animation.
func AuditAnimation(anim *lottie.Animation) []LayerCapabilityReport {
	if anim == nil {
		return nil
	}
	reports := make([]LayerCapabilityReport, len(anim.Layers))
	for i := range anim.Layers {
		l := &anim.Layers[i]
		fallback, reason := ShouldFallbackLayer(l)
		reports[i] = LayerCapabilityReport{
			LayerIndex:       l.Index,
			LayerName:        l.Name,
			LayerType:        l.Type,
			IsVectorEligible: !fallback,
			Reason:           reason,
		}
	}
	return reports
}

// computeTightBounds finds the tight non-zero alpha bounding box of an image.
func computeTightBounds(img image.Image) (minX, minY, maxX, maxY int, hasVisible bool) {
	bounds := img.Bounds()
	minX, minY = bounds.Max.X, bounds.Max.Y
	maxX, maxY = bounds.Min.X, bounds.Min.Y

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
				hasVisible = true
			}
		}
	}

	if !hasVisible {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX + 1, maxY + 1, true
}

// cropTightImage crops an image to the specified rectangle.
func cropTightImage(src image.Image, minX, minY, maxX, maxY int) image.Image {
	rect := image.Rect(0, 0, maxX-minX, maxY-minY)
	dst := image.NewRGBA(rect)
	draw.Draw(dst, rect, src, image.Pt(minX, minY), draw.Src)
	return dst
}

// BakeIsolatedLayer renders only a single isolated layer (plus its parent transform chain)
// into a cropped WebP sprite, returning the tight bounding box and offset.
func BakeIsolatedLayer(anim *lottie.Animation, layerIndex int, opts RenderOptions) ([]byte, int32, int32, float32, float32, error) {
	if anim == nil || layerIndex < 0 || layerIndex >= len(anim.Layers) {
		return nil, 0, 0, 0, 0, errors.New("invalid layer index for isolated baking")
	}

	targetLayer := anim.Layers[layerIndex]

	// 1. Build an isolated Lottie animation containing ONLY targetLayer and empty parent transforms
	parentSet := make(map[int]bool)
	currParent := targetLayer.Parent
	for currParent > 0 {
		parentSet[currParent] = true
		for _, l := range anim.Layers {
			if l.Index == currParent {
				currParent = l.Parent
				break
			}
		}
	}

	var isolatedLayers []lottie.Layer
	for _, l := range anim.Layers {
		if l.Index == targetLayer.Index {
			isolatedLayers = append(isolatedLayers, l)
		} else if parentSet[l.Index] {
			// Include parent as a null transform layer with empty shapes to provide world coordinates without pixels
			parentStub := l
			parentStub.Shapes = json.RawMessage("[]")
			parentStub.Type = 3 // Null layer
			isolatedLayers = append(isolatedLayers, parentStub)
		}
	}

	isolatedAnim := &lottie.Animation{
		Version:   anim.Version,
		FrameRate: anim.FrameRate,
		InPoint:   anim.InPoint,
		OutPoint:  anim.OutPoint,
		Width:     anim.Width,
		Height:    anim.Height,
		Assets:    anim.Assets,
		Layers:    isolatedLayers,
	}

	isolatedJSON, err := json.Marshal(isolatedAnim)
	if err != nil {
		return nil, 0, 0, 0, 0, fmt.Errorf("failed serializing isolated layer: %w", err)
	}
	isolatedAnim.RawJSONBytes = isolatedJSON

	// 2. Discover browser for rendering
	browserPath := opts.EdgePath
	if browserPath == "" {
		if found, err := FindBrowserExecutable(""); err == nil {
			browserPath = found
		}
	}

	if browserPath == "" {
		// Pure Go transparent 1x1 fallback if no browser is installed
		return createTransparent1x1WebP()
	}

	// 3. Render single frame of isolated animation via headless browser
	renderedPNG, err := renderIsolatedFrameBrowser(isolatedAnim, browserPath, opts)
	if err != nil {
		return createTransparent1x1WebP()
	}

	// 4. Decode PNG and compute tight bounding box (Alpha Cropping)
	img, _, err := image.Decode(bytes.NewReader(renderedPNG))
	if err != nil {
		return createTransparent1x1WebP()
	}

	minX, minY, maxX, maxY, hasVisible := computeTightBounds(img)
	if !hasVisible {
		return createTransparent1x1WebP()
	}

	// 5. Crop to tight bounds
	cropped := cropTightImage(img, minX, minY, maxX, maxY)
	cropW := int32(maxX - minX)
	cropH := int32(maxY - minY)

	var rawPNG bytes.Buffer
	if err := png.Encode(&rawPNG, cropped); err == nil {
		if webpBytes, _, _, err := EncodeToWebP(rawPNG.Bytes(), opts.Quality); err == nil {
			return webpBytes, cropW, cropH, float32(minX), float32(minY), nil
		}
	}

	return createTransparent1x1WebP()
}

// renderIsolatedFrameBrowser renders a single frame of the isolated animation using headless Edge/Chrome.
func renderIsolatedFrameBrowser(anim *lottie.Animation, browserPath string, opts RenderOptions) ([]byte, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	var mu sync.Mutex
	var frameData []byte
	doneCh := make(chan struct{})

	mux := http.NewServeMux()

	mux.HandleFunc("/lottie.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(embeddedLottieJS)
	})

	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(anim.RawJSONBytes)
	})

	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		b64 := r.FormValue("data")
		if b64 != "" {
			if dec, err := base64.StdEncoding.DecodeString(b64); err == nil {
				frameData = dec
				select {
				case <-doneCh:
				default:
					close(doneCh)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	})

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
    async function run() {
        try {
            const resp = await fetch('/data.json');
            const data = await resp.json();
            const anim = lottie.loadAnimation({
                container: document.getElementById('anim'),
                renderer: 'canvas',
                loop: false,
                autoplay: false,
                animationData: data
            });
            anim.addEventListener('DOMLoaded', () => {
                anim.goToAndStop(0, true);
                setTimeout(() => {
                    const canvas = document.querySelector('canvas');
                    if (canvas) {
                        const dataUrl = canvas.toDataURL('image/png');
                        const b64 = dataUrl.split(',')[1];
                        const fd = new FormData();
                        fd.append('data', b64);
                        fetch('/upload', { method: 'POST', body: fd });
                    }
                }, 50);
            });
        } catch(e) {}
    }
    run();
    </script>
</body>
</html>`, anim.Width, anim.Height)

	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	cmd := exec.Command(
		browserPath,
		"--headless=new",
		"--disable-gpu",
		"--mute-audio",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--no-first-run",
		fmt.Sprintf("http://127.0.0.1:%d/index.html", port),
	)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	select {
	case <-doneCh:
	case <-ctx.Done():
		KillProcessTree(cmd)
		return nil, errors.New("timeout baking isolated layer")
	}

	KillProcessTree(cmd)

	mu.Lock()
	defer mu.Unlock()
	if len(frameData) == 0 {
		return nil, errors.New("empty frame data captured")
	}
	return frameData, nil
}

// createTransparent1x1WebP returns a minimal 1x1 transparent WebP image.
func createTransparent1x1WebP() ([]byte, int32, int32, float32, float32, error) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{0, 0, 0, 0})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err == nil {
		if webp, _, _, err := EncodeToWebP(buf.Bytes(), 0.80); err == nil {
			return webp, 1, 1, 0, 0, nil
		}
	}
	// Fallback raw 1x1 RIFF WebP header
	raw1x1 := []byte{
		0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50,
		0x56, 0x50, 0x38, 0x4c, 0x0e, 0x00, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00,
		0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe, 0x07, 0x00,
	}
	return raw1x1, 1, 1, 0, 0, nil
}

// ConvertLottieHybrid executes the complete Selective Hybrid Conversion Pipeline.
// Standard vector layers are compiled directly to resolution-independent PAG vectors;
// layers with complex effects, expressions, or unsupported operators are baked selectively
// as tightly bounded transparent WebP sprites (Tag 49), all assembled in a single unified VectorComposition.
func ConvertLottieHybrid(anim *lottie.Animation, opts RenderOptions) ([]byte, error) {
	if anim == nil {
		return nil, errors.New("nil animation provided")
	}

	container := pag.NewContainer()

	// 1. Tag 32: TimeStretchMode (Scale = 1)
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	// 2. Process and emit unique embedded Image Assets (Tag 49 WebP)
	assetIDToImageID := make(map[string]uint32)
	nextImageID := uint32(1)

	for _, asset := range anim.Assets {
		if asset.ID == "" || asset.File == "" {
			continue
		}

		rawBytes, err := decodeAssetBytes(asset.File)
		if err != nil || len(rawBytes) == 0 {
			continue
		}

		width := asset.Width
		height := asset.Height
		if width <= 0 || height <= 0 {
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(rawBytes)); err == nil {
				width = int32(cfg.Width)
				height = int32(cfg.Height)
			}
		}
		if width <= 0 {
			width = 100
		}
		if height <= 0 {
			height = 100
		}

		webpBytes := rawBytes
		if !strings.HasPrefix(string(rawBytes[:min(4, len(rawBytes))]), "RIFF") {
			if converted, w, h, err := EncodeToWebP(rawBytes, opts.Quality); err == nil {
				webpBytes = converted
				width = w
				height = h
			}
		}

		imageTag, err := pag.BuildImageBytesV3(nextImageID, webpBytes, width, height, 0, 0)
		if err == nil {
			container.AddTag(pag.TagImageBytesV3, imageTag)
			assetIDToImageID[asset.ID] = nextImageID
			nextImageID++
		}
	}

	// 3. Process Layers with Selective Hybrid Routing
	totalDuration := anim.DurationFrames()
	if totalDuration == 0 {
		totalDuration = 1
	}

	indMap := make(map[int]*lottie.Layer, len(anim.Layers))
	for i := range anim.Layers {
		if anim.Layers[i].Index > 0 {
			indMap[anim.Layers[i].Index] = &anim.Layers[i]
		}
	}

	var layers [][]byte

	for i, lottieLayer := range anim.Layers {
		layerID := uint32(100 + i)
		layerName := lottieLayer.Name
		if layerName == "" {
			layerName = fmt.Sprintf("layer_%d", i)
		}

		startTime := uint64(0)
		if lottieLayer.StartTime > 0 {
			startTime = uint64(lottieLayer.StartTime)
		} else if lottieLayer.InPoint > 0 {
			startTime = uint64(lottieLayer.InPoint)
		}

		duration := uint64(0)
		if lottieLayer.OutPoint > lottieLayer.InPoint {
			duration = uint64(lottieLayer.OutPoint - lottieLayer.InPoint)
		} else {
			duration = totalDuration
		}

		posX, posY, scaleX, scaleY := resolveLayerWorldTransform(&anim.Layers[i], indMap)

		fallback, reason := ShouldFallbackLayer(&anim.Layers[i])
		if fallback {
			// ==========================================
			// SELECTIVE RASTERIZATION FALLBACK
			// ==========================================
			fmt.Printf("    [Hybrid] Layer #%d (%s): Selective Fallback -> %s\n", i, layerName, reason)
			isolatedWebP, bboxW, bboxH, offX, offY, bakeErr := BakeIsolatedLayer(anim, i, opts)
			if bakeErr == nil && len(isolatedWebP) > 0 {
				imageID := nextImageID
				nextImageID++

				imgTag, err := pag.BuildImageBytesV3(imageID, isolatedWebP, bboxW, bboxH, 0, 0)
				if err == nil {
					container.AddTag(pag.TagImageBytesV3, imgTag)

					effPosX := posX + offX
					effPosY := posY + offY
					imgLayer, err := pag.BuildImageLayerWithTransform(layerID, imageID, layerName, startTime, duration, effPosX, effPosY, 1.0, 1.0)
					if err == nil {
						layers = append(layers, imgLayer)
						continue
					}
				}
			}
		}

		// ==========================================
		// PURE VECTOR PATH
		// ==========================================
		switch lottieLayer.Type {
		case 1: // Solid Layer
			r, g, b := parseHexColor(lottieLayer.SolidColor)
			sw := lottieLayer.SolidWidth
			sh := lottieLayer.SolidHeight
			if sw <= 0 {
				sw = anim.Width
			}
			if sh <= 0 {
				sh = anim.Height
			}
			solidPayload, err := pag.BuildSolidLayer(layerID, layerName, duration, sw, sh, r, g, b)
			if err == nil {
				layers = append(layers, solidPayload)
			}

		case 2: // Image Layer
			if imgID, exists := assetIDToImageID[lottieLayer.RefID]; exists {
				imgPayload, err := pag.BuildImageLayerWithTransform(layerID, imgID, layerName, startTime, duration, posX, posY, scaleX, scaleY)
				if err == nil {
					layers = append(layers, imgPayload)
				}
			}

		case 4: // Vector Shape Layer
			shapeElements := parseLottieShapeElements(lottieLayer.Shapes)
			shapePayload, err := pag.BuildShapeLayerWithTransform(layerID, layerName, startTime, duration, posX, posY, scaleX, scaleY, shapeElements)
			if err == nil {
				layers = append(layers, shapePayload)
			}

		default:
			// Fallback: create empty transform layer if needed
		}
	}

	// Tag 94: ImageScaleModes (if images present)
	if nextImageID > 1 {
		container.AddTag(pag.TagImageScaleModes, []byte{0x02, 0x02, 0x02})
	}

	// Tag 2: Unified VectorComposition
	compPayload, err := pag.BuildVectorComposition(0, anim.Width, anim.Height, totalDuration, anim.FrameRate, layers)
	if err != nil {
		return nil, fmt.Errorf("failed compiling hybrid VectorComposition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}
