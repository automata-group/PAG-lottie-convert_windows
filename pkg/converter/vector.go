package converter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"

	"lottie2pag/pkg/lottie"
	"lottie2pag/pkg/pag"
)

// ConvertLottieDirectVector exports a Lottie animation directly into a native Tencent PAG
// VectorComposition with zero full-canvas rasterization. It maps Lottie vector shape layers,
// solid layers, and embedded image assets directly into resolution-independent PAG vector tags.
func ConvertLottieDirectVector(anim *lottie.Animation, opts RenderOptions) ([]byte, error) {
	if anim == nil {
		return nil, errors.New("nil animation provided")
	}

	container := pag.NewContainer()

	// 1. Tag 32: TimeStretchMode (Scale = 1)
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	// 2. Process and emit unique Image Assets (Tag 48 / Tag 49)
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

		// Emit Tag 48 (ImageBytesV2 - PNG) or Tag 49 (ImageBytesV3 - WebP)
		var imageTag []byte
		if strings.HasPrefix(string(rawBytes[:min(4, len(rawBytes))]), "RIFF") {
			imageTag, err = pag.BuildImageBytesV3(nextImageID, rawBytes, width, height, 0, 0)
			if err == nil {
				container.AddTag(pag.TagImageBytesV3, imageTag)
			}
		} else {
			imageTag, err = pag.BuildImageBytesV2(nextImageID, rawBytes, width, height, 0, 0)
			if err == nil {
				container.AddTag(pag.TagImageBytesV2, imageTag)
			}
		}

		if err == nil {
			assetIDToImageID[asset.ID] = nextImageID
			nextImageID++
		}
	}

	// 3. Process Layers into native PAG Layer sub-tags
	totalDuration := anim.DurationFrames()
	if totalDuration == 0 {
		totalDuration = 1
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

		posX, posY, scaleX, scaleY := parseLottieTransform(lottieLayer.Transform)

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
			shapePayload, err := pag.BuildShapeLayer(layerID, layerName, duration, posX, posY, shapeElements)
			if err == nil {
				layers = append(layers, shapePayload)
			}

		default:
			// Fallback: create empty transform layer if needed
		}
	}

	// Tag 94: Layer Markers
	container.AddTag(pag.TagLayerMarkers, []byte{0x02, 0x02, 0x02})

	// Tag 2: VectorComposition compiling all native layers
	compPayload, err := pag.BuildVectorComposition(0, anim.Width, anim.Height, totalDuration, anim.FrameRate, layers)
	if err != nil {
		return nil, fmt.Errorf("failed compiling VectorComposition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}

// decodeAssetBytes handles embedded data URIs (base64) or direct raw data.
func decodeAssetBytes(fileStr string) ([]byte, error) {
	if strings.HasPrefix(fileStr, "data:") {
		idx := strings.Index(fileStr, ",")
		if idx >= 0 {
			b64 := fileStr[idx+1:]
			return base64.StdEncoding.DecodeString(b64)
		}
	}
	return base64.StdEncoding.DecodeString(fileStr)
}

// parseLottieTransform extracts static translation and scaling from ks object.
func parseLottieTransform(raw json.RawMessage) (posX, posY, scaleX, scaleY float32) {
	scaleX = 1.0
	scaleY = 1.0
	if len(raw) == 0 {
		return
	}

	var ks struct {
		P struct {
			K json.RawMessage `json:"k"`
		} `json:"p"`
		S struct {
			K json.RawMessage `json:"k"`
		} `json:"s"`
	}

	if err := json.Unmarshal(raw, &ks); err != nil {
		return
	}

	// Parse Position
	if len(ks.P.K) > 0 {
		var posArr []float64
		if err := json.Unmarshal(ks.P.K, &posArr); err == nil && len(posArr) >= 2 {
			posX = float32(posArr[0])
			posY = float32(posArr[1])
		}
	}

	// Parse Scale
	if len(ks.S.K) > 0 {
		var scaleArr []float64
		if err := json.Unmarshal(ks.S.K, &scaleArr); err == nil && len(scaleArr) >= 2 {
			scaleX = float32(scaleArr[0] / 100.0)
			scaleY = float32(scaleArr[1] / 100.0)
		}
	}

	return
}

// parseHexColor parses "#RRGGBB" or "RRGGBB" hex strings into RGB bytes.
func parseHexColor(s string) (byte, byte, byte) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 6 {
		r, _ := strconv.ParseUint(s[0:2], 16, 8)
		g, _ := strconv.ParseUint(s[2:4], 16, 8)
		b, _ := strconv.ParseUint(s[4:6], 16, 8)
		return byte(r), byte(g), byte(b)
	}
	return 255, 255, 255
}

// parseLottieShapeElements converts Lottie shapes JSON array into PAG shape element payloads.
func parseLottieShapeElements(raw json.RawMessage) [][]byte {
	if len(raw) == 0 {
		return nil
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	var elements [][]byte
	for _, it := range items {
		ty, _ := it["ty"].(string)
		switch ty {
		case "fl": // Fill
			if fillTag := buildFillFromLottie(it); len(fillTag) > 0 {
				elements = append(elements, fillTag)
			}
		case "st": // Stroke
			if strokeTag := buildStrokeFromLottie(it); len(strokeTag) > 0 {
				elements = append(elements, strokeTag)
			}
		case "gr": // Group
			if grpItems, ok := it["it"].([]interface{}); ok {
				subJSON, err := json.Marshal(grpItems)
				if err == nil {
					subElements := parseLottieShapeElements(subJSON)
					if len(subElements) > 0 {
						if grpPayload, err := pag.BuildShapeGroup(subElements); err == nil {
							elements = append(elements, grpPayload)
						}
					}
				}
			}
		}
	}

	return elements
}

func buildFillFromLottie(m map[string]interface{}) []byte {
	r, g, b := byte(255), byte(255), byte(255)
	opacity := byte(255)

	if cObj, ok := m["c"].(map[string]interface{}); ok {
		if kArr, ok := cObj["k"].([]interface{}); ok && len(kArr) >= 3 {
			rf, _ := kArr[0].(float64)
			gf, _ := kArr[1].(float64)
			bf, _ := kArr[2].(float64)
			r = byte(clamp01(rf) * 255.0)
			g = byte(clamp01(gf) * 255.0)
			b = byte(clamp01(bf) * 255.0)
		}
	}

	if oObj, ok := m["o"].(map[string]interface{}); ok {
		if of, ok := oObj["k"].(float64); ok {
			opacity = byte((clamp01(of / 100.0)) * 255.0)
		}
	}

	tag, err := pag.BuildFill(r, g, b, opacity)
	if err != nil {
		return nil
	}
	return tag
}

func buildStrokeFromLottie(m map[string]interface{}) []byte {
	r, g, b := byte(0), byte(0), byte(0)
	opacity := byte(255)
	width := float32(2.0)

	if cObj, ok := m["c"].(map[string]interface{}); ok {
		if kArr, ok := cObj["k"].([]interface{}); ok && len(kArr) >= 3 {
			rf, _ := kArr[0].(float64)
			gf, _ := kArr[1].(float64)
			bf, _ := kArr[2].(float64)
			r = byte(clamp01(rf) * 255.0)
			g = byte(clamp01(gf) * 255.0)
			b = byte(clamp01(bf) * 255.0)
		}
	}

	if wObj, ok := m["w"].(map[string]interface{}); ok {
		if wf, ok := wObj["k"].(float64); ok && wf > 0 {
			width = float32(wf)
		}
	}

	tag, err := pag.BuildStroke(r, g, b, opacity, width)
	if err != nil {
		return nil
	}
	return tag
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
