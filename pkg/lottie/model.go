package lottie

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseFile reads a Lottie file (.json or .lottie ZIP bundle) from disk and parses it.
func ParseFile(filePath string) (*Animation, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Detect .lottie / ZIP archive format (PK\x03\x04 signature)
	if len(data) > 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04 {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("failed opening .lottie zip bundle: %w", err)
		}
		var animFile *zip.File
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".json") && !strings.EqualFold(f.Name, "manifest.json") {
				animFile = f
				break
			}
		}
		if animFile == nil {
			return nil, fmt.Errorf("no animation JSON found inside .lottie bundle %s", filePath)
		}
		rc, err := animFile.Open()
		if err != nil {
			return nil, fmt.Errorf("failed opening inner animation JSON: %w", err)
		}
		defer rc.Close()
		animData, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("failed reading inner animation JSON: %w", err)
		}
		return Parse(animData)
	}

	return Parse(data)
}

// Animation represents the root Lottie JSON schema.
type Animation struct {
	Version      string          `json:"v"`
	FrameRate    float32         `json:"fr"`
	InPoint      float32         `json:"ip"`
	OutPoint     float32         `json:"op"`
	Width        int32           `json:"w"`
	Height       int32           `json:"h"`
	Name         string          `json:"nm,omitempty"`
	DDD          int             `json:"ddd,omitempty"`
	Assets       []Asset         `json:"assets,omitempty"`
	Layers       []Layer         `json:"layers"`
	Markers      json.RawMessage `json:"markers,omitempty"`
	Chars        json.RawMessage `json:"chars,omitempty"`
	RawJSONBytes []byte          `json:"-"`
}

// Asset represents pre-composed layers or raster/vector image assets.
type Asset struct {
	ID     string  `json:"id"`
	Width  int32   `json:"w,omitempty"`
	Height int32   `json:"h,omitempty"`
	Path   string  `json:"u,omitempty"`
	File   string  `json:"p,omitempty"`
	Layers []Layer `json:"layers,omitempty"`
}

// Layer represents a Lottie layer item (shape, solid, null, image, precomp).
type Layer struct {
	Type        int             `json:"ty"` // 0: Precomp, 1: Solid, 2: Image, 3: Null, 4: Shape, 5: Text
	Name        string          `json:"nm"`
	Index       int             `json:"ind"`
	Parent      int             `json:"parent,omitempty"`
	InPoint     float32         `json:"ip"`
	OutPoint    float32         `json:"op"`
	StartTime   float32         `json:"st"`
	BlendMode   int             `json:"bm,omitempty"`
	RefID       string          `json:"refId,omitempty"` // For image or precomp
	SolidColor  string          `json:"sc,omitempty"`    // For solid layers, hex e.g. "#ffffff"
	SolidWidth  int32           `json:"sw,omitempty"`
	SolidHeight int32           `json:"sh,omitempty"`
	Shapes      json.RawMessage `json:"shapes,omitempty"`
	Transform   json.RawMessage `json:"ks,omitempty"`
	AutoOrient  int             `json:"ao,omitempty"`
}

// Parse parses a Lottie JSON byte slice into Animation.
func Parse(data []byte) (*Animation, error) {
	var anim Animation
	if err := json.Unmarshal(data, &anim); err != nil {
		return nil, fmt.Errorf("failed to parse Lottie JSON: %w", err)
	}
	if anim.Width <= 0 || anim.Height <= 0 {
		return nil, fmt.Errorf("invalid Lottie dimensions: %dx%d", anim.Width, anim.Height)
	}
	if anim.FrameRate <= 0 {
		anim.FrameRate = 30.0
	}
	anim.RawJSONBytes = data
	return &anim, nil
}


// DurationFrames returns the duration in frames (op - ip).
func (a *Animation) DurationFrames() uint64 {
	if a.OutPoint > a.InPoint {
		return uint64(a.OutPoint - a.InPoint)
	}
	return 1
}

// DurationMicroseconds returns duration in microseconds.
func (a *Animation) DurationMicroseconds() uint64 {
	frames := float64(a.DurationFrames())
	fps := float64(a.FrameRate)
	if fps <= 0 {
		fps = 30.0
	}
	return uint64((frames / fps) * 1000000.0)
}
