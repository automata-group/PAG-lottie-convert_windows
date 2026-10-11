package converter

import (
	"encoding/json"
	"math"

	"lottie2pag/pkg/pag"
)

type lottieEaseJSON struct {
	X json.RawMessage `json:"x"`
	Y json.RawMessage `json:"y"`
}

type lottieKeyframeJSON struct {
	T *float64        `json:"t"` // Frame number
	S json.RawMessage `json:"s"` // Start value
	E json.RawMessage `json:"e"` // End value (optional)
	I *lottieEaseJSON `json:"i"` // In tangent
	O *lottieEaseJSON `json:"o"` // Out tangent
	H *int            `json:"h"` // Hold flag
}

type lottiePropertyJSON struct {
	A int             `json:"a"` // 0 = static, 1 = animated
	K json.RawMessage `json:"k"` // Value or keyframe array
	S bool            `json:"s"` // Separated dimensions
	X struct {
		K json.RawMessage `json:"k"`
	} `json:"x"`
	Y struct {
		K json.RawMessage `json:"k"`
	} `json:"y"`
}

// ParseLottieTransform parses the full animated or static 2D transform from Lottie's `ks` object.
func ParseLottieTransform(raw json.RawMessage, layerDuration uint64) pag.Transform2DData {
	tr := pag.NewDefaultTransform2DData()
	if len(raw) == 0 {
		return tr
	}

	var ks struct {
		P lottiePropertyJSON `json:"p"`
		A lottiePropertyJSON `json:"a"`
		S lottiePropertyJSON `json:"s"`
		R lottiePropertyJSON `json:"r"`
		RZ lottiePropertyJSON `json:"rz"`
		O lottiePropertyJSON `json:"o"`
	}

	if err := json.Unmarshal(raw, &ks); err != nil {
		return tr
	}

	// 1. Position (Type 5)
	posKfs, posX, posY := parsePointProperty(ks.P, false, layerDuration)
	tr.PosKfs = posKfs
	tr.PosX = posX
	tr.PosY = posY

	// 2. Anchor Point (Type 5)
	ancKfs, ancX, ancY := parsePointProperty(ks.A, false, layerDuration)
	tr.AnchorKfs = ancKfs
	tr.AnchorX = ancX
	tr.AnchorY = ancY

	// 3. Scale (Type 4, percentage -> factor)
	scaleKfs, scX, scY := parsePointProperty(ks.S, true, layerDuration)
	tr.ScaleKfs = scaleKfs
	if len(scaleKfs) == 0 && (scX != 0 || scY != 0) {
		tr.ScaleX = scX
		tr.ScaleY = scY
	} else if len(scaleKfs) == 0 {
		tr.ScaleX = 1.0
		tr.ScaleY = 1.0
	}

	// 4. Rotation (Type 2, degrees)
	rotProp := ks.R
	if len(rotProp.K) == 0 && len(ks.RZ.K) > 0 {
		rotProp = ks.RZ
	}
	rotKfs, rotVal := parseFloatProperty(rotProp, layerDuration)
	tr.RotKfs = rotKfs
	tr.Rotation = rotVal

	// 5. Opacity (Type 2, 0..100 -> 0..255)
	alphaKfs, alphaVal := parseByteProperty(ks.O, layerDuration)
	tr.AlphaKfs = alphaKfs
	tr.Opacity = alphaVal

	return tr
}

func parsePointProperty(prop lottiePropertyJSON, isScale bool, layerDuration uint64) ([]pag.PointKeyframe, float32, float32) {
	if len(prop.K) == 0 {
		if isScale {
			return nil, 1.0, 1.0
		}
		return nil, 0, 0
	}

	var rawKfs []lottieKeyframeJSON
	if err := json.Unmarshal(prop.K, &rawKfs); err == nil && len(rawKfs) > 0 {
		kfs := buildPointKeyframes(rawKfs, isScale, layerDuration)
		if len(kfs) > 0 {
			return kfs, 0, 0
		}
		// Fallback to static from first keyframe
		if x, y, ok := parseVec2(rawKfs[0].S, isScale); ok {
			return nil, x, y
		}
	}

	// Static property
	if x, y, ok := parseVec2(prop.K, isScale); ok {
		return nil, x, y
	}

	if isScale {
		return nil, 1.0, 1.0
	}
	return nil, 0, 0
}

func parseFloatProperty(prop lottiePropertyJSON, layerDuration uint64) ([]pag.FloatKeyframe, float32) {
	if len(prop.K) == 0 {
		return nil, 0
	}

	var rawKfs []lottieKeyframeJSON
	if err := json.Unmarshal(prop.K, &rawKfs); err == nil && len(rawKfs) > 0 {
		kfs := buildFloatKeyframes(rawKfs, layerDuration)
		if len(kfs) > 0 {
			return kfs, 0
		}
		if val, ok := parseScalar(rawKfs[0].S); ok {
			return nil, val
		}
	}

	if val, ok := parseScalar(prop.K); ok {
		return nil, val
	}
	return nil, 0
}

func parseByteProperty(prop lottiePropertyJSON, layerDuration uint64) ([]pag.ByteKeyframe, uint8) {
	if len(prop.K) == 0 {
		return nil, 255
	}

	var rawKfs []lottieKeyframeJSON
	if err := json.Unmarshal(prop.K, &rawKfs); err == nil && len(rawKfs) > 0 {
		kfs := buildByteKeyframes(rawKfs, layerDuration)
		if len(kfs) > 0 {
			return kfs, 255
		}
		if b, ok := parseOpacityByte(rawKfs[0].S); ok {
			return nil, b
		}
	}

	if b, ok := parseOpacityByte(prop.K); ok {
		return nil, b
	}
	return nil, 255
}

func buildPointKeyframes(rawKfs []lottieKeyframeJSON, isScale bool, layerDuration uint64) []pag.PointKeyframe {
	M := len(rawKfs)
	if M < 2 {
		if M == 1 && len(rawKfs[0].E) > 0 {
			sX, sY, sOk := parseVec2(rawKfs[0].S, isScale)
			eX, eY, eOk := parseVec2(rawKfs[0].E, isScale)
			if sOk && eOk {
				t0 := uint64(0)
				if rawKfs[0].T != nil && *rawKfs[0].T > 0 {
					t0 = uint64(math.Round(*rawKfs[0].T))
				}
				dur := uint64(1)
				if layerDuration > t0 {
					dur = layerDuration - t0
				}
				interp, ease := parseEasing(rawKfs[0])
				res := make([]pag.PointKeyframe, 0, 2)
				if t0 > 0 {
					res = append(res, pag.PointKeyframe{
						StartTime: 0,
						Duration:  t0,
						StartVal:  [2]float32{sX, sY},
						EndVal:    [2]float32{sX, sY},
						Interp:    0,
					})
				}
				res = append(res, pag.PointKeyframe{
					StartTime: t0,
					Duration:  dur,
					StartVal:  [2]float32{sX, sY},
					EndVal:    [2]float32{eX, eY},
					Interp:    interp,
					Ease:      ease,
				})
				return res
			}
		}
		return nil
	}

	kfs := make([]pag.PointKeyframe, 0, M+2)

	firstT := float64(0)
	if rawKfs[0].T != nil {
		firstT = *rawKfs[0].T
	}
	firstStart := uint64(math.Round(firstT))
	if firstStart > 0 {
		sX0, sY0, _ := parseVec2(rawKfs[0].S, isScale)
		kfs = append(kfs, pag.PointKeyframe{
			StartTime: 0,
			Duration:  firstStart,
			StartVal:  [2]float32{sX0, sY0},
			EndVal:    [2]float32{sX0, sY0},
			Interp:    0, // Hold
		})
	}

	for j := 0; j < M-1; j++ {
		sX, sY, sOk := parseVec2(rawKfs[j].S, isScale)
		if !sOk {
			continue
		}
		var eX, eY float32
		var eOk bool
		if len(rawKfs[j].E) > 0 {
			eX, eY, eOk = parseVec2(rawKfs[j].E, isScale)
		} else {
			eX, eY, eOk = parseVec2(rawKfs[j+1].S, isScale)
		}
		if !eOk {
			eX, eY = sX, sY
		}

		tCur := float64(0)
		if rawKfs[j].T != nil {
			tCur = *rawKfs[j].T
		}
		tNext := tCur + 1
		if rawKfs[j+1].T != nil {
			tNext = *rawKfs[j+1].T
		}

		startFrame := uint64(math.Round(tCur))
		nextFrame := uint64(math.Round(tNext))
		dur := uint64(1)
		if nextFrame > startFrame {
			dur = nextFrame - startFrame
		}

		interp, ease := parseEasing(rawKfs[j])
		kfs = append(kfs, pag.PointKeyframe{
			StartTime: startFrame,
			Duration:  dur,
			StartVal:  [2]float32{sX, sY},
			EndVal:    [2]float32{eX, eY},
			Interp:    interp,
			Ease:      ease,
		})
	}

	if len(kfs) > 0 && layerDuration > 0 {
		lastKf := kfs[len(kfs)-1]
		lastEnd := lastKf.StartTime + lastKf.Duration
		if lastEnd < layerDuration {
			kfs = append(kfs, pag.PointKeyframe{
				StartTime: lastEnd,
				Duration:  layerDuration - lastEnd,
				StartVal:  lastKf.EndVal,
				EndVal:    lastKf.EndVal,
				Interp:    0, // Hold
			})
		}
	}

	return kfs
}

func buildFloatKeyframes(rawKfs []lottieKeyframeJSON, layerDuration uint64) []pag.FloatKeyframe {
	M := len(rawKfs)
	if M < 2 {
		if M == 1 && len(rawKfs[0].E) > 0 {
			sVal, sOk := parseScalar(rawKfs[0].S)
			eVal, eOk := parseScalar(rawKfs[0].E)
			if sOk && eOk {
				t0 := uint64(0)
				if rawKfs[0].T != nil && *rawKfs[0].T > 0 {
					t0 = uint64(math.Round(*rawKfs[0].T))
				}
				dur := uint64(1)
				if layerDuration > t0 {
					dur = layerDuration - t0
				}
				interp, ease := parseEasing(rawKfs[0])
				res := make([]pag.FloatKeyframe, 0, 2)
				if t0 > 0 {
					res = append(res, pag.FloatKeyframe{
						StartTime: 0,
						Duration:  t0,
						StartVal:  sVal,
						EndVal:    sVal,
						Interp:    0,
					})
				}
				res = append(res, pag.FloatKeyframe{
					StartTime: t0,
					Duration:  dur,
					StartVal:  sVal,
					EndVal:    eVal,
					Interp:    interp,
					Ease:      ease,
				})
				return res
			}
		}
		return nil
	}

	kfs := make([]pag.FloatKeyframe, 0, M+2)

	firstT := float64(0)
	if rawKfs[0].T != nil {
		firstT = *rawKfs[0].T
	}
	firstStart := uint64(math.Round(firstT))
	if firstStart > 0 {
		sVal0, _ := parseScalar(rawKfs[0].S)
		kfs = append(kfs, pag.FloatKeyframe{
			StartTime: 0,
			Duration:  firstStart,
			StartVal:  sVal0,
			EndVal:    sVal0,
			Interp:    0, // Hold
		})
	}

	for j := 0; j < M-1; j++ {
		sVal, sOk := parseScalar(rawKfs[j].S)
		if !sOk {
			continue
		}
		var eVal float32
		var eOk bool
		if len(rawKfs[j].E) > 0 {
			eVal, eOk = parseScalar(rawKfs[j].E)
		} else {
			eVal, eOk = parseScalar(rawKfs[j+1].S)
		}
		if !eOk {
			eVal = sVal
		}

		tCur := float64(0)
		if rawKfs[j].T != nil {
			tCur = *rawKfs[j].T
		}
		tNext := tCur + 1
		if rawKfs[j+1].T != nil {
			tNext = *rawKfs[j+1].T
		}

		startFrame := uint64(math.Round(tCur))
		nextFrame := uint64(math.Round(tNext))
		dur := uint64(1)
		if nextFrame > startFrame {
			dur = nextFrame - startFrame
		}

		interp, ease := parseEasing(rawKfs[j])
		kfs = append(kfs, pag.FloatKeyframe{
			StartTime: startFrame,
			Duration:  dur,
			StartVal:  sVal,
			EndVal:    eVal,
			Interp:    interp,
			Ease:      ease,
		})
	}

	if len(kfs) > 0 && layerDuration > 0 {
		lastKf := kfs[len(kfs)-1]
		lastEnd := lastKf.StartTime + lastKf.Duration
		if lastEnd < layerDuration {
			kfs = append(kfs, pag.FloatKeyframe{
				StartTime: lastEnd,
				Duration:  layerDuration - lastEnd,
				StartVal:  lastKf.EndVal,
				EndVal:    lastKf.EndVal,
				Interp:    0, // Hold
			})
		}
	}

	return kfs
}

func buildByteKeyframes(rawKfs []lottieKeyframeJSON, layerDuration uint64) []pag.ByteKeyframe {
	M := len(rawKfs)
	if M < 2 {
		if M == 1 && len(rawKfs[0].E) > 0 {
			sVal, sOk := parseOpacityByte(rawKfs[0].S)
			eVal, eOk := parseOpacityByte(rawKfs[0].E)
			if sOk && eOk {
				t0 := uint64(0)
				if rawKfs[0].T != nil && *rawKfs[0].T > 0 {
					t0 = uint64(math.Round(*rawKfs[0].T))
				}
				dur := uint64(1)
				if layerDuration > t0 {
					dur = layerDuration - t0
				}
				interp, ease := parseEasing(rawKfs[0])
				res := make([]pag.ByteKeyframe, 0, 2)
				if t0 > 0 {
					res = append(res, pag.ByteKeyframe{
						StartTime: 0,
						Duration:  t0,
						StartVal:  sVal,
						EndVal:    sVal,
						Interp:    0,
					})
				}
				res = append(res, pag.ByteKeyframe{
					StartTime: t0,
					Duration:  dur,
					StartVal:  sVal,
					EndVal:    eVal,
					Interp:    interp,
					Ease:      ease,
				})
				return res
			}
		}
		return nil
	}

	kfs := make([]pag.ByteKeyframe, 0, M+2)

	firstT := float64(0)
	if rawKfs[0].T != nil {
		firstT = *rawKfs[0].T
	}
	firstStart := uint64(math.Round(firstT))
	if firstStart > 0 {
		sVal0, _ := parseOpacityByte(rawKfs[0].S)
		kfs = append(kfs, pag.ByteKeyframe{
			StartTime: 0,
			Duration:  firstStart,
			StartVal:  sVal0,
			EndVal:    sVal0,
			Interp:    0, // Hold
		})
	}

	for j := 0; j < M-1; j++ {
		sVal, sOk := parseOpacityByte(rawKfs[j].S)
		if !sOk {
			continue
		}
		var eVal uint8
		var eOk bool
		if len(rawKfs[j].E) > 0 {
			eVal, eOk = parseOpacityByte(rawKfs[j].E)
		} else {
			eVal, eOk = parseOpacityByte(rawKfs[j+1].S)
		}
		if !eOk {
			eVal = sVal
		}

		tCur := float64(0)
		if rawKfs[j].T != nil {
			tCur = *rawKfs[j].T
		}
		tNext := tCur + 1
		if rawKfs[j+1].T != nil {
			tNext = *rawKfs[j+1].T
		}

		startFrame := uint64(math.Round(tCur))
		nextFrame := uint64(math.Round(tNext))
		dur := uint64(1)
		if nextFrame > startFrame {
			dur = nextFrame - startFrame
		}

		interp, ease := parseEasing(rawKfs[j])
		kfs = append(kfs, pag.ByteKeyframe{
			StartTime: startFrame,
			Duration:  dur,
			StartVal:  sVal,
			EndVal:    eVal,
			Interp:    interp,
			Ease:      ease,
		})
	}

	if len(kfs) > 0 && layerDuration > 0 {
		lastKf := kfs[len(kfs)-1]
		lastEnd := lastKf.StartTime + lastKf.Duration
		if lastEnd < layerDuration {
			kfs = append(kfs, pag.ByteKeyframe{
				StartTime: lastEnd,
				Duration:  layerDuration - lastEnd,
				StartVal:  lastKf.EndVal,
				EndVal:    lastKf.EndVal,
				Interp:    0, // Hold
			})
		}
	}

	return kfs
}

func parseEasing(kf lottieKeyframeJSON) (uint8, [4]float32) {
	if kf.H != nil && *kf.H == 1 {
		return 0, [4]float32{} // Hold
	}
	if kf.I != nil && kf.O != nil {
		ox := parseEaseFloat(kf.O.X)
		oy := parseEaseFloat(kf.O.Y)
		ix := parseEaseFloat(kf.I.X)
		iy := parseEaseFloat(kf.I.Y)
		return 2, [4]float32{ox, oy, ix, iy} // Bezier
	}
	return 1, [4]float32{} // Linear
}

func parseEaseFloat(raw json.RawMessage) float32 {
	if len(raw) == 0 {
		return 0
	}
	var f float32
	if err := json.Unmarshal(raw, &f); err == nil {
		return f
	}
	var arr []float32
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0]
	}
	return 0
}

func parseVec2(raw json.RawMessage, isScale bool) (float32, float32, bool) {
	if len(raw) == 0 {
		return 0, 0, false
	}
	var arr []float32
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) >= 2 {
		x, y := arr[0], arr[1]
		if isScale {
			x /= 100.0
			y /= 100.0
		}
		return x, y, true
	}
	return 0, 0, false
}

func parseScalar(raw json.RawMessage) (float32, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var f float32
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}
	var arr []float32
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0], true
	}
	return 0, false
}

func parseOpacityByte(raw json.RawMessage) (uint8, bool) {
	if f, ok := parseScalar(raw); ok {
		if f < 0 {
			f = 0
		}
		if f > 100 {
			f = 100
		}
		b := uint8(math.Round(float64(f) * 255.0 / 100.0))
		return b, true
	}
	return 255, false
}
