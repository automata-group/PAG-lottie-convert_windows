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
	"math"
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

		// Emit Tag 49 (ImageBytesV3 - WebP) for universal libpag support
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

	// 3. Process Layers into native PAG Layer sub-tags
	totalDuration := anim.DurationFrames()
	if totalDuration == 0 {
		totalDuration = 1
	}

	// Build ind map for multi-level parent hierarchy resolution
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

	// Tag 94: ImageScaleModes (required by libpag when images are present)
	if len(assetIDToImageID) > 0 {
		container.AddTag(pag.TagImageScaleModes, []byte{0x02, 0x02, 0x02})
	}

	// Layer Markers (only if present in Lottie)
	if len(anim.Markers) > 0 && string(anim.Markers) != "[]" && string(anim.Markers) != "null" {
		container.AddTag(pag.TagLayerMarkers, []byte{0x02, 0x02, 0x02})
	}

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

type lottieRawKeyframe struct {
	S json.RawMessage `json:"s"`
}

func parseRawVec2(raw json.RawMessage) (x, y float64, ok bool) {
	if len(raw) == 0 {
		return 0, 0, false
	}
	var flat []float64
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) >= 2 {
		return flat[0], flat[1], true
	}
	var kfs []lottieRawKeyframe
	if err := json.Unmarshal(raw, &kfs); err == nil && len(kfs) > 0 {
		if err := json.Unmarshal(kfs[0].S, &flat); err == nil && len(flat) >= 2 {
			return flat[0], flat[1], true
		}
	}
	return 0, 0, false
}

func parseRawScalar(raw json.RawMessage) (val float64, ok bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, true
	}
	var arr []float64
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0], true
	}
	var kfs []lottieRawKeyframe
	if err := json.Unmarshal(raw, &kfs); err == nil && len(kfs) > 0 {
		if err := json.Unmarshal(kfs[0].S, &num); err == nil {
			return num, true
		}
		if err := json.Unmarshal(kfs[0].S, &arr); err == nil && len(arr) > 0 {
			return arr[0], true
		}
	}
	return 0, false
}

// parseLayerTransform extracts static translation, anchor point, scale, and rotation from ks object.
func parseLayerTransform(raw json.RawMessage) (posX, posY, anchorX, anchorY, scaleX, scaleY, rot float64) {
	scaleX = 1.0
	scaleY = 1.0
	if len(raw) == 0 {
		return
	}

	var ks struct {
		P struct {
			K json.RawMessage `json:"k"`
			S bool            `json:"s"` // Separated dimensions
			X struct {
				K json.RawMessage `json:"k"`
			} `json:"x"`
			Y struct {
				K json.RawMessage `json:"k"`
			} `json:"y"`
		} `json:"p"`
		A struct {
			K json.RawMessage `json:"k"`
		} `json:"a"`
		S struct {
			K json.RawMessage `json:"k"`
		} `json:"s"`
		R struct {
			K json.RawMessage `json:"k"`
		} `json:"r"`
		RZ struct {
			K json.RawMessage `json:"k"`
		} `json:"rz"`
	}

	if err := json.Unmarshal(raw, &ks); err != nil {
		return
	}

	// Position
	if ks.P.S {
		if xVal, ok := parseRawScalar(ks.P.X.K); ok {
			posX = xVal
		}
		if yVal, ok := parseRawScalar(ks.P.Y.K); ok {
			posY = yVal
		}
	} else {
		if px, py, ok := parseRawVec2(ks.P.K); ok {
			posX = px
			posY = py
		}
	}

	// Anchor point
	if ax, ay, ok := parseRawVec2(ks.A.K); ok {
		anchorX = ax
		anchorY = ay
	}

	// Scale (Lottie scale is in percentages, 100% = 1.0)
	if sx, sy, ok := parseRawVec2(ks.S.K); ok {
		scaleX = sx / 100.0
		scaleY = sy / 100.0
	}

	// Rotation (degrees)
	if rVal, ok := parseRawScalar(ks.R.K); ok {
		rot = rVal
	} else if rzVal, ok := parseRawScalar(ks.RZ.K); ok {
		rot = rzVal
	}

	return
}

// resolveLayerWorldTransform walks up the parent hierarchy chain to accumulate 2D affine
// matrix transforms (translation, rotation, scale, anchor point) into exact world coordinates.
func resolveLayerWorldTransform(l *lottie.Layer, indMap map[int]*lottie.Layer) (worldX, worldY, scaleX, scaleY float32) {
	var chain []*lottie.Layer
	curr := l
	visited := make(map[int]bool)
	for curr != nil {
		chain = append(chain, curr)
		if curr.Index > 0 {
			visited[curr.Index] = true
		}
		if curr.Parent > 0 && !visited[curr.Parent] {
			curr = indMap[curr.Parent]
		} else {
			break
		}
	}

	// Reverse chain: Root ancestor -> ... -> Leaf
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	// 2D affine matrix: [a, c, tx; b, d, ty]
	a, b, c, d := 1.0, 0.0, 0.0, 1.0
	tx, ty := 0.0, 0.0

	for _, node := range chain {
		px, py, ax, ay, sx, sy, rot := parseLayerTransform(node.Transform)

		rad := rot * math.Pi / 180.0
		cosR := math.Cos(rad)
		sinR := math.Sin(rad)

		la := cosR * sx
		lb := sinR * sx
		lc := -sinR * sy
		ld := cosR * sy

		ltx := px - (la*ax + lc*ay)
		lty := py - (lb*ax + ld*ay)

		// Affine concatenation: M_world = M_parent * M_local
		na := a*la + c*lb
		nb := b*la + d*lb
		nc := a*lc + c*ld
		nd := b*lc + d*ld
		ntx := a*ltx + c*lty + tx
		nty := b*ltx + d*lty + ty

		a, b, c, d = na, nb, nc, nd
		tx, ty = ntx, nty
	}

	worldX = float32(tx)
	worldY = float32(ty)

	scX := math.Sqrt(a*a + b*b)
	scY := math.Sqrt(c*c + d*d)
	if a < 0 {
		scX = -scX
	}
	if d < 0 {
		scY = -scY
	}
	scaleX = float32(scX)
	scaleY = float32(scY)
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
		case "sh": // Bezier Shape Path
			if pathTag := buildShapePathFromLottie(it); len(pathTag) > 0 {
				elements = append(elements, pathTag)
			}
		case "rc": // Rectangle
			if rectTag := buildRectangleFromLottie(it); len(rectTag) > 0 {
				elements = append(elements, rectTag)
			}
		case "el": // Ellipse
			if elTag := buildEllipseFromLottie(it); len(elTag) > 0 {
				elements = append(elements, elTag)
			}
		case "fl": // Fill
			if fillTag := buildFillFromLottie(it); len(fillTag) > 0 {
				elements = append(elements, fillTag)
			}
		case "gf": // Gradient Fill (maps to primary gradient tone)
			if gfTag := buildGradientFillFromLottie(it); len(gfTag) > 0 {
				elements = append(elements, gfTag)
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

func buildShapePathFromLottie(m map[string]interface{}) []byte {
	ksObj, ok := m["ks"].(map[string]interface{})
	if !ok {
		return nil
	}

	var shapeMap map[string]interface{}
	kVal := ksObj["k"]
	if kMap, ok := kVal.(map[string]interface{}); ok {
		shapeMap = kMap
	} else if kArr, ok := kVal.([]interface{}); ok && len(kArr) > 0 {
		if firstKf, ok := kArr[0].(map[string]interface{}); ok {
			if sArr, ok := firstKf["s"].([]interface{}); ok && len(sArr) > 0 {
				if sObj, ok := sArr[0].(map[string]interface{}); ok {
					shapeMap = sObj
				}
			}
		}
	}

	if shapeMap == nil {
		return nil
	}

	vArr, _ := shapeMap["v"].([]interface{})
	if len(vArr) == 0 {
		return nil
	}

	iArr, _ := shapeMap["i"].([]interface{})
	oArr, _ := shapeMap["o"].([]interface{})
	closed, _ := shapeMap["c"].(bool)

	points := make([][2]float32, 0, len(vArr))
	inTangents := make([][2]float32, 0, len(vArr))
	outTangents := make([][2]float32, 0, len(vArr))

	for idx, ptRaw := range vArr {
		var pt [2]float32
		if ptCoords, ok := ptRaw.([]interface{}); ok && len(ptCoords) >= 2 {
			x, _ := ptCoords[0].(float64)
			y, _ := ptCoords[1].(float64)
			pt = [2]float32{float32(x), float32(y)}
		}
		points = append(points, pt)

		var inT [2]float32
		if idx < len(iArr) {
			if inCoords, ok := iArr[idx].([]interface{}); ok && len(inCoords) >= 2 {
				ix, _ := inCoords[0].(float64)
				iy, _ := inCoords[1].(float64)
				inT = [2]float32{float32(ix), float32(iy)}
			}
		}
		inTangents = append(inTangents, inT)

		var outT [2]float32
		if idx < len(oArr) {
			if outCoords, ok := oArr[idx].([]interface{}); ok && len(outCoords) >= 2 {
				ox, _ := outCoords[0].(float64)
				oy, _ := outCoords[1].(float64)
				outT = [2]float32{float32(ox), float32(oy)}
			}
		}
		outTangents = append(outTangents, outT)
	}

	verbs := make([]byte, 0, len(points)+2)
	coords := make([]float32, 0, len(points)*6)

	// Move
	verbs = append(verbs, pag.PathVerbMove)
	coords = append(coords, points[0][0], points[0][1])

	// Subsequent vertices
	for n := 1; n < len(points); n++ {
		prevOut := outTangents[n-1]
		currIn := inTangents[n]
		currPt := points[n]

		if prevOut[0] == 0 && prevOut[1] == 0 && currIn[0] == 0 && currIn[1] == 0 {
			verbs = append(verbs, pag.PathVerbLine)
			coords = append(coords, currPt[0], currPt[1])
		} else {
			cp1X := points[n-1][0] + prevOut[0]
			cp1Y := points[n-1][1] + prevOut[1]
			cp2X := currPt[0] + currIn[0]
			cp2Y := currPt[1] + currIn[1]
			verbs = append(verbs, pag.PathVerbCurve11)
			coords = append(coords, cp1X, cp1Y, cp2X, cp2Y, currPt[0], currPt[1])
		}
	}

	// Close
	if closed && len(points) > 1 {
		lastIdx := len(points) - 1
		lastOut := outTangents[lastIdx]
		firstIn := inTangents[0]
		firstPt := points[0]

		if lastOut[0] != 0 || lastOut[1] != 0 || firstIn[0] != 0 || firstIn[1] != 0 {
			cp1X := points[lastIdx][0] + lastOut[0]
			cp1Y := points[lastIdx][1] + lastOut[1]
			cp2X := firstPt[0] + firstIn[0]
			cp2Y := firstPt[1] + firstIn[1]
			verbs = append(verbs, pag.PathVerbCurve11)
			coords = append(coords, cp1X, cp1Y, cp2X, cp2Y, firstPt[0], firstPt[1])
		}
		verbs = append(verbs, pag.PathVerbClose)
	}

	tag, err := pag.BuildShapePath(verbs, coords)
	if err != nil {
		return nil
	}
	return tag
}

func buildRectangleFromLottie(m map[string]interface{}) []byte {
	var w, h float32 = 100, 100
	var posX, posY float32
	var roundness float32

	if sObj, ok := m["s"].(map[string]interface{}); ok {
		if kArr, ok := sObj["k"].([]interface{}); ok && len(kArr) >= 2 {
			wf, _ := kArr[0].(float64)
			hf, _ := kArr[1].(float64)
			w = float32(wf)
			h = float32(hf)
		}
	}
	if pObj, ok := m["p"].(map[string]interface{}); ok {
		if kArr, ok := pObj["k"].([]interface{}); ok && len(kArr) >= 2 {
			px, _ := kArr[0].(float64)
			py, _ := kArr[1].(float64)
			posX = float32(px)
			posY = float32(py)
		}
	}
	if rObj, ok := m["r"].(map[string]interface{}); ok {
		if rf, ok := rObj["k"].(float64); ok {
			roundness = float32(rf)
		}
	}

	tag, err := pag.BuildRectangle(w, h, posX, posY, roundness)
	if err != nil {
		return nil
	}
	return tag
}

func buildEllipseFromLottie(m map[string]interface{}) []byte {
	var w, h float32 = 100, 100
	var posX, posY float32

	if sObj, ok := m["s"].(map[string]interface{}); ok {
		if kArr, ok := sObj["k"].([]interface{}); ok && len(kArr) >= 2 {
			wf, _ := kArr[0].(float64)
			hf, _ := kArr[1].(float64)
			w = float32(wf)
			h = float32(hf)
		}
	}
	if pObj, ok := m["p"].(map[string]interface{}); ok {
		if kArr, ok := pObj["k"].([]interface{}); ok && len(kArr) >= 2 {
			px, _ := kArr[0].(float64)
			py, _ := kArr[1].(float64)
			posX = float32(px)
			posY = float32(py)
		}
	}

	tag, err := pag.BuildEllipse(w, h, posX, posY)
	if err != nil {
		return nil
	}
	return tag
}

func buildGradientFillFromLottie(m map[string]interface{}) []byte {
	r, g, b := byte(128), byte(128), byte(128)
	opacity := byte(255)

	if gObj, ok := m["g"].(map[string]interface{}); ok {
		if kArr, ok := gObj["k"].([]interface{}); ok && len(kArr) >= 4 {
			rf, _ := kArr[1].(float64)
			gf, _ := kArr[2].(float64)
			bf, _ := kArr[3].(float64)
			r = byte(clamp01(rf) * 255.0)
			g = byte(clamp01(gf) * 255.0)
			b = byte(clamp01(bf) * 255.0)
		}
	}

	if oObj, ok := m["o"].(map[string]interface{}); ok {
		if of, ok := oObj["k"].(float64); ok {
			opacity = byte(clamp01(of/100.0) * 255.0)
		}
	}

	tag, err := pag.BuildFill(r, g, b, opacity)
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
