package pag

import (
	"encoding/json"
	"fmt"
	"os"

	"lottie2pag/pkg/compress"
)

// PAGInfo contains structured metadata extracted from a PAG file.
type PAGInfo struct {
	FilePath       string        `json:"file_path,omitempty"`
	FileSize       int           `json:"file_size"`
	IsCompressed   bool          `json:"is_compressed,omitempty"`
	CompressedSize int           `json:"compressed_size,omitempty"`
	Magic          string        `json:"magic"`
	BodyLength     uint32        `json:"body_length"`
	TagsCount      int           `json:"tags_count"`
	Compositions   []CompSummary `json:"compositions,omitempty"`
	ImagesCount    int           `json:"images_count"`
	VideosCount    int           `json:"videos_count"`
	Tags           []TagSummary  `json:"tags"`
}

// CompSummary describes a composition inside the PAG file.
type CompSummary struct {
	Type           string   `json:"type"` // "Vector", "Video", "Bitmap"
	ID             uint64   `json:"id"`
	Width          int64    `json:"width"`
	Height         int64    `json:"height"`
	DurationFrames uint64   `json:"duration_frames"`
	FPS            float32  `json:"fps"`
	LayersCount    int      `json:"layers_count"`
	LayerNames     []string `json:"layer_names,omitempty"`
}

// TagSummary describes an encoded tag.
type TagSummary struct {
	Index  int    `json:"index"`
	Code   uint16 `json:"code"`
	Name   string `json:"name"`
	Length int    `json:"length"`
}

// TagName returns the human-readable string for a Tag code.
func TagName(code uint16) string {
	switch code {
	case TagEnd:
		return "EndTag"
	case TagFontTables:
		return "FontTables"
	case TagVectorComposition:
		return "VectorComposition"
	case TagCompositionAttributes:
		return "CompositionAttributes"
	case TagImageTables:
		return "ImageTables"
	case TagLayer:
		return "Layer"
	case TagLayerAttributesV1:
		return "LayerAttributesV1"
	case TagSolidColor:
		return "SolidColor"
	case TagTextSource:
		return "TextSource"
	case TagTextMoreOption:
		return "TextMoreOption"
	case TagImageReference:
		return "ImageReference"
	case TagCompositionReference:
		return "CompositionReference"
	case TagTransform2D:
		return "Transform2D"
	case TagMaskBlock:
		return "MaskBlock"
	case TagShapeGroup:
		return "ShapeGroup"
	case TagRectangle:
		return "Rectangle"
	case TagEllipse:
		return "Ellipse"
	case TagPolyStar:
		return "PolyStar"
	case TagShapePath:
		return "ShapePath"
	case TagFill:
		return "Fill"
	case TagStroke:
		return "Stroke"
	case TagGradientFill:
		return "GradientFill"
	case TagGradientStroke:
		return "GradientStroke"
	case TagMergePaths:
		return "MergePaths"
	case TagTrimPaths:
		return "TrimPaths"
	case TagRepeater:
		return "Repeater"
	case TagRoundCorners:
		return "RoundCorners"
	case TagPerformance:
		return "Performance"
	case TagDropShadowStyle:
		return "DropShadowStyle"
	case TagCachePolicy:
		return "CachePolicy"
	case TagFileAttributes:
		return "FileAttributes"
	case TagTimeStretchMode:
		return "TimeStretchMode"
	case TagMp4Header:
		return "Mp4Header"
	case TagBitmapComposition:
		return "BitmapComposition"
	case TagBitmapSequence:
		return "BitmapSequence"
	case TagImageBytesV1:
		return "ImageBytesV1"
	case TagImageBytesV2:
		return "ImageBytesV2"
	case TagImageBytesV3:
		return "ImageBytesV3"
	case TagVideoComposition:
		return "VideoComposition"
	case TagVideoSequence:
		return "VideoSequence"
	case TagLayerAttributes:
		return "LayerAttributesV2"
	case TagMarkerList:
		return "MarkerList"
	case TagImageFillRule:
		return "ImageFillRule"
	case TagAudioBytes:
		return "AudioBytes"
	case TagLayerAttributesV3:
		return "LayerAttributesV3"
	case TagLayerAttributesExtra:
		return "LayerAttributesExtra"
	case TagImageScaleModes:
		return "ImageScaleModes"
	default:
		return fmt.Sprintf("Tag_%d", code)
	}
}

// InspectFile reads a PAG file and parses its metadata.
func InspectFile(filePath string) (*PAGInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	info, err := InspectBytes(data)
	if err != nil {
		return nil, err
	}
	info.FilePath = filePath
	return info, nil
}

// InspectBytes parses PAG binary data and returns PAGInfo.
func InspectBytes(data []byte) (*PAGInfo, error) {
	if compress.IsZstd(data) {
		decomp, err := compress.DecompressZstd(data)
		if err != nil {
			return nil, fmt.Errorf("failed decompressing Zstandard stream: %w", err)
		}
		info, err := InspectBytes(decomp)
		if err != nil {
			return nil, err
		}
		info.IsCompressed = true
		info.CompressedSize = len(data)
		return info, nil
	}

	container, err := Decode(data)
	if err != nil {
		return nil, err
	}

	info := &PAGInfo{
		FileSize:     len(data),
		Magic:        string(data[:4]),
		BodyLength:   uint32(len(data) - 9),
		TagsCount:    len(container.Tags),
		Compositions: make([]CompSummary, 0),
		Tags:         make([]TagSummary, 0, len(container.Tags)),
	}

	for idx, tag := range container.Tags {
		summary := TagSummary{
			Index:  idx,
			Code:   tag.Code,
			Name:   TagName(tag.Code),
			Length: len(tag.Payload),
		}
		info.Tags = append(info.Tags, summary)

		switch tag.Code {
		case TagImageBytesV1, TagImageBytesV2, TagImageBytesV3:
			info.ImagesCount++
		case TagVideoComposition:
			info.VideosCount++
			comp := parseVideoCompositionSummary(tag.Payload)
			info.Compositions = append(info.Compositions, comp)
		case TagVectorComposition:
			comp := parseVectorCompositionSummary(tag.Payload)
			info.Compositions = append(info.Compositions, comp)
		}
	}

	return info, nil
}

func parseVectorCompositionSummary(payload []byte) CompSummary {
	s := NewBitStreamFromBytes(payload)
	compID, _ := s.ReadEncodedUint()
	comp := CompSummary{
		Type:       "Vector",
		ID:         compID,
		LayerNames: make([]string, 0),
	}

	for s.Len() > 0 {
		code, length, err := ReadTagHeader(s)
		if err != nil || code == TagEnd {
			break
		}
		subData := make([]byte, length)
		s.Read(subData)

		if code == TagCompositionAttributes {
			attrs := NewBitStreamFromBytes(subData)
			comp.Width, _ = attrs.ReadEncodedInt()
			comp.Height, _ = attrs.ReadEncodedInt()
			comp.DurationFrames, _ = attrs.ReadTime()
			comp.FPS, _ = attrs.ReadFloat32()
		} else if code == TagLayer {
			comp.LayersCount++
			ls := NewBitStreamFromBytes(subData)
			ls.ReadByte() // layerType
			ls.ReadEncodedUint() // layerID
			for ls.Len() > 0 {
				sc, sl, _ := ReadTagHeader(ls)
				if sc == TagEnd {
					break
				}
				sd := make([]byte, sl)
				ls.Read(sd)
				if sc == TagLayerAttributes && len(sd) > 3 {
					// String starts at index 3
					nameEnd := len(sd)
					for i := 3; i < len(sd); i++ {
						if sd[i] == 0 {
							nameEnd = i
							break
						}
					}
					if nameEnd > 3 {
						comp.LayerNames = append(comp.LayerNames, string(sd[3:nameEnd]))
					}
				}
			}
		}
	}
	return comp
}

func parseVideoCompositionSummary(payload []byte) CompSummary {
	return CompSummary{
		Type: "Video",
	}
}

// ToJSON returns PAGInfo formatted as pretty JSON.
func (p *PAGInfo) ToJSON() (string, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
