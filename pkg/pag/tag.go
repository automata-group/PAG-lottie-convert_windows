package pag

import (
	"errors"
	"fmt"
)

// Standard PAG Tag Codes from official Tencent libpag specification (include/pag/file.h).
const (
	TagEnd                   uint16 = 0
	TagFontTables            uint16 = 1
	TagVectorComposition    uint16 = 2
	TagCompositionAttributes uint16 = 3
	TagImageTables           uint16 = 4
	TagLayer                 uint16 = 5
	TagLayerAttributesV1     uint16 = 6
	TagSolidColor            uint16 = 7
	TagTextSource            uint16 = 8
	TagTextMoreOption        uint16 = 10
	TagImageReference        uint16 = 11
	TagCompositionReference  uint16 = 12
	TagTransform2D           uint16 = 13
	TagMaskBlock             uint16 = 14
	TagShapeGroup            uint16 = 15
	TagRectangle             uint16 = 16
	TagEllipse               uint16 = 17
	TagPolyStar              uint16 = 18
	TagShapePath             uint16 = 19
	TagFill                  uint16 = 20
	TagStroke                uint16 = 21
	TagGradientFill          uint16 = 22
	TagGradientStroke        uint16 = 23
	TagMergePaths            uint16 = 24
	TagTrimPaths             uint16 = 25
	TagRepeater              uint16 = 26
	TagRoundCorners          uint16 = 27
	TagPerformance           uint16 = 28
	TagDropShadowStyle       uint16 = 29
	TagCachePolicy           uint16 = 30
	TagFileAttributes        uint16 = 31
	TagTimeStretchMode       uint16 = 32
	TagMp4Header             uint16 = 33
	TagBitmapComposition    uint16 = 45
	TagBitmapSequence        uint16 = 46
	TagImageBytesV1          uint16 = 47
	TagImageBytesV2          uint16 = 48
	TagImageBytesV3          uint16 = 49
	TagVideoComposition      uint16 = 50
	TagVideoSequence         uint16 = 51
	TagLayerAttributes       uint16 = 52 // LayerAttributesV2
	TagMarkerList            uint16 = 53
	TagImageFillRule         uint16 = 54
	TagAudioBytes            uint16 = 55
	TagLayerAttributesV3     uint16 = 62
	TagLayerAttributesExtra  uint16 = 63
	TagTextSourceV2          uint16 = 64
	TagTextSourceV3          uint16 = 68
	TagTextPathOption        uint16 = 69
	TagTextAnimator          uint16 = 70
	TagImageScaleModes       uint16 = 94

	// Backward-compatibility aliases
	TagTransform      = TagLayerAttributesV1
	TagLayerTransform = TagTransform2D
	TagLayerMarkers   = TagImageScaleModes
)

// Layer Types inside TagLayer (TagCode 5).
const (
	LayerTypeNull       byte = 0
	LayerTypeSolid      byte = 1
	LayerTypeText       byte = 2
	LayerTypeShape      byte = 4
	LayerTypeImage      byte = 5
	LayerTypePreCompose byte = 6
	LayerTypeCamera     byte = 7
)

// TimeStretchMode constants
const (
	TimeStretchNone   uint32 = 0
	TimeStretchScale  uint32 = 1
	TimeStretchRepeat uint32 = 2
)

// Path verbs for ShapePath.
const (
	PathVerbClose   byte = 0
	PathVerbMove    byte = 1
	PathVerbLine    byte = 2
	PathVerbHLine   byte = 3
	PathVerbVLine   byte = 4
	PathVerbCurve01 byte = 5
	PathVerbCurve10 byte = 6
	PathVerbCurve11 byte = 7
)

// Tag represents a single encoded block in the PAG tag stream.
type Tag struct {
	Code    uint16
	Payload []byte
}

// WriteTagHeader writes a tag header according to Tencent specification:
// - If payload length < 63: 2 bytes (tagCode << 6 | length)
// - If payload length >= 63: 2 bytes (tagCode << 6 | 0x3F) followed by 4 bytes uint32 length.
func WriteTagHeader(w *BitStream, tagCode uint16, length uint32) error {
	if length < 63 {
		raw := (tagCode << 6) | uint16(length)
		return w.WriteUint16(raw)
	}
	raw := (tagCode << 6) | 0x3F
	if err := w.WriteUint16(raw); err != nil {
		return err
	}
	return w.WriteUint32(length)
}

// ReadTagHeader parses the next tag code and payload length from stream.
func ReadTagHeader(r *BitStream) (uint16, uint32, error) {
	raw, err := r.ReadUint16()
	if err != nil {
		return 0, 0, err
	}
	tagCode := raw >> 6
	lenFlag := uint32(raw & 0x3F)
	if lenFlag < 63 {
		return tagCode, lenFlag, nil
	}
	length, err := r.ReadUint32()
	if err != nil {
		return 0, 0, err
	}
	return tagCode, length, nil
}

// WriteTag serializes a Tag into the bitstream with its header.
func WriteTag(w *BitStream, tagCode uint16, payload []byte) error {
	if err := WriteTagHeader(w, tagCode, uint32(len(payload))); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.WriteBytes(payload); err != nil {
			return err
		}
	}
	return nil
}

// BuildFileAttributes creates Tag 31 (FileAttributes) payload.
func BuildFileAttributes(appVersion, aeVersion string) []byte {
	s := NewBitStream()
	if appVersion == "" {
		appVersion = "lottie2pag-go"
	}
	s.WriteByte(byte(len(appVersion)))
	s.WriteBytes([]byte(appVersion))
	return s.Bytes()
}

// BuildTimeStretchMode creates Tag 32 payload (default mode: Scale = 1).
func BuildTimeStretchMode(mode uint32) []byte {
	s := NewBitStream()
	s.WriteUint32(mode)
	return s.Bytes()
}

// BuildImageBytesV3 creates Tag 49 (ImageBytesV3) payload containing WebP data.
func BuildImageBytesV3(id uint32, webpData []byte, width, height, anchorX, anchorY int32) ([]byte, error) {
	if len(webpData) == 0 {
		return nil, errors.New("empty webp data for ImageBytesV3")
	}
	s := NewBitStream()
	// 1. Image ID
	if err := s.WriteEncodedUint(uint64(id)); err != nil {
		return nil, err
	}
	// 2. Byte count of WebP data (LEB128)
	if err := s.WriteEncodedUint(uint64(len(webpData))); err != nil {
		return nil, err
	}
	// 3. Raw WebP binary data
	if _, err := s.WriteBytes(webpData); err != nil {
		return nil, err
	}
	// 4. Scale (1.0f)
	if err := s.WriteFloat32(1.0); err != nil {
		return nil, err
	}
	// 5. Width (> 0 required by ImageBytes::verify)
	if err := s.WriteEncodedInt(int64(width)); err != nil {
		return nil, err
	}
	// 6. Height (> 0 required by ImageBytes::verify)
	if err := s.WriteEncodedInt(int64(height)); err != nil {
		return nil, err
	}
	// 7. Anchor X
	if err := s.WriteEncodedInt(int64(anchorX)); err != nil {
		return nil, err
	}
	// 8. Anchor Y
	if err := s.WriteEncodedInt(int64(anchorY)); err != nil {
		return nil, err
	}
	return s.Bytes(), nil
}

// BuildImageBytesV2 creates Tag 48 (ImageBytesV2) payload containing PNG data.
func BuildImageBytesV2(id uint32, pngData []byte) ([]byte, error) {
	if len(pngData) == 0 {
		return nil, errors.New("empty png data for ImageBytesV2")
	}
	s := NewBitStream()
	// 1. Image ID
	if err := s.WriteEncodedUint(uint64(id)); err != nil {
		return nil, err
	}
	// 2. Byte count of PNG data (LEB128)
	if err := s.WriteEncodedUint(uint64(len(pngData))); err != nil {
		return nil, err
	}
	// 3. Raw PNG binary data
	if _, err := s.WriteBytes(pngData); err != nil {
		return nil, err
	}
	// 4. Scale (1.0f)
	if err := s.WriteFloat32(1.0); err != nil {
		return nil, err
	}
	return s.Bytes(), nil
}

// BuildCompositionAttributes creates SubTag 3 (CompositionAttributes) inside a composition.
func BuildCompositionAttributes(width, height int32, durationFrames uint64, fps float32, r, g, b byte) ([]byte, error) {
	s := NewBitStream()
	if err := s.WriteEncodedInt(int64(width)); err != nil {
		return nil, err
	}
	if err := s.WriteEncodedInt(int64(height)); err != nil {
		return nil, err
	}
	if err := s.WriteTime(durationFrames); err != nil {
		return nil, err
	}
	if err := s.WriteFloat32(fps); err != nil {
		return nil, err
	}
	if err := s.WriteColor(r, g, b); err != nil {
		return nil, err
	}
	return s.Bytes(), nil
}

// BuildLayerAttributesWithTime creates SubTag 52 payload for a layer with explicit startTime and duration.
// When startTime == 0:
//   Flag Byte 1 = 0x01 (visible)
//   Flag Byte 2 = 0x01 (duration present)
//   duration in frames (LEB128)
//   name + 0x00 (null-terminated string)
// When startTime > 0:
//   Flag Byte 1 = 0x11 (visible | startTime present via Bit 4)
//   Flag Byte 2 = 0x01 (duration present)
//   startTime in frames (LEB128)
//   duration in frames (LEB128)
//   name + 0x00 (null-terminated string)
func BuildLayerAttributesWithTime(name string, startTime, durationFrames uint64) []byte {
	s := NewBitStream()
	if startTime == 0 {
		s.WriteByte(0x01)
		s.WriteByte(0x01)
		s.WriteTime(durationFrames)
	} else {
		s.WriteByte(0x11)
		s.WriteByte(0x01)
		s.WriteTime(startTime)
		s.WriteTime(durationFrames)
	}
	s.WriteBytes([]byte(name))
	s.WriteByte(0x00)
	return s.Bytes()
}

// BuildLayerAttributes creates SubTag 52 payload for a layer starting at frame 0.
func BuildLayerAttributes(name string, durationFrames uint64) []byte {
	return BuildLayerAttributesWithTime(name, 0, durationFrames)
}

// BuildDefaultTransform creates SubTag 13 payload for layer transform.
// Format: bitflags (0x02 indicates position is present) + float32 X + float32 Y.
func BuildDefaultTransform(posX, posY float32) []byte {
	s := NewBitStream()
	s.WriteByte(0x02)
	s.WriteFloat32(posX)
	s.WriteFloat32(posY)
	return s.Bytes()
}

// BuildTransform2D creates SubTag 13 (Transform2D) with complete position and scale attributes.
func BuildTransform2D(posX, posY, scaleX, scaleY float32) []byte {
	flags := NewBitStream()
	content := NewBitStream()

	// 1. anchorPoint (default {0,0}) -> flag = 0
	flags.WriteBitBoolean(false)

	// 2. position (if not {0,0}) -> flag = 1
	if posX != 0 || posY != 0 {
		flags.WriteBitBoolean(true)  // exist
		flags.WriteBitBoolean(false) // animatable
		flags.WriteBitBoolean(false) // hasSpatial
		content.WriteFloat32(posX)
		content.WriteFloat32(posY)
	} else {
		flags.WriteBitBoolean(false)
	}

	// 3. xPosition (0)
	flags.WriteBitBoolean(false)
	// 4. yPosition (0)
	flags.WriteBitBoolean(false)

	// 5. scale (if not {1,1}) -> flag = 1
	if scaleX != 1.0 || scaleY != 1.0 {
		flags.WriteBitBoolean(true)  // exist
		flags.WriteBitBoolean(false) // animatable
		content.WriteFloat32(scaleX)
		content.WriteFloat32(scaleY)
	} else {
		flags.WriteBitBoolean(false)
	}

	// 6. rotation (0)
	flags.WriteBitBoolean(false)
	// 7. opacity (255 / Opaque default)
	flags.WriteBitBoolean(false)

	flags.AlignWithBytes()
	out := NewBitStream()
	out.WriteBytes(flags.Bytes())
	out.WriteBytes(content.Bytes())
	return out.Bytes()
}

// BuildImageLayerWithTransform creates SubTag 5 (LayerTag) referencing an image asset with position and scale transforms.
func BuildImageLayerWithTransform(layerID, imageID uint32, layerName string, startTime, durationFrames uint64, posX, posY, scaleX, scaleY float32) ([]byte, error) {
	s := NewBitStream()
	// LayerType = 5 (ImageLayer)
	if err := s.WriteByte(LayerTypeImage); err != nil {
		return nil, err
	}
	// Layer ID
	if err := s.WriteEncodedUint(uint64(layerID)); err != nil {
		return nil, err
	}

	// SubTag 52: LayerAttributes
	attrPayload := BuildLayerAttributesWithTime(layerName, startTime, durationFrames)
	if err := WriteTag(s, TagLayerAttributes, attrPayload); err != nil {
		return nil, err
	}

	// SubTag 13: Transform
	var transPayload []byte
	if scaleX != 1.0 || scaleY != 1.0 || posX != 0 || posY != 0 {
		transPayload = BuildTransform2D(posX, posY, scaleX, scaleY)
	} else {
		transPayload = BuildDefaultTransform(posX, posY)
	}
	if err := WriteTag(s, TagLayerTransform, transPayload); err != nil {
		return nil, err
	}

	// SubTag 11: ImageReference
	refStream := NewBitStream()
	if err := refStream.WriteEncodedUint(uint64(imageID)); err != nil {
		return nil, err
	}
	if err := WriteTag(s, TagImageReference, refStream.Bytes()); err != nil {
		return nil, err
	}

	// Layer EndTag
	if err := WriteTag(s, TagEnd, nil); err != nil {
		return nil, err
	}

	return s.Bytes(), nil
}

// BuildImageLayerWithTime creates SubTag 5 (LayerTag) referencing an image asset with custom startTime and duration.
func BuildImageLayerWithTime(layerID, imageID uint32, layerName string, startTime, durationFrames uint64, posX, posY float32) ([]byte, error) {
	return BuildImageLayerWithTransform(layerID, imageID, layerName, startTime, durationFrames, posX, posY, 1.0, 1.0)
}

// BuildImageLayer creates SubTag 5 (LayerTag) referencing an image asset starting at frame 0.
func BuildImageLayer(layerID, imageID uint32, layerName string, durationFrames uint64, posX, posY float32) ([]byte, error) {
	return BuildImageLayerWithTime(layerID, imageID, layerName, 0, durationFrames, posX, posY)
}


// BuildShapeLayerWithTransform creates SubTag 5 (LayerTag) for a native vector Shape Layer with custom time and 2D transform (pos & scale).
func BuildShapeLayerWithTransform(layerID uint32, layerName string, startTime, durationFrames uint64, posX, posY, scaleX, scaleY float32, shapeTags [][]byte) ([]byte, error) {
	s := NewBitStream()
	// LayerType = 4 (ShapeLayer)
	if err := s.WriteByte(LayerTypeShape); err != nil {
		return nil, err
	}
	if err := s.WriteEncodedUint(uint64(layerID)); err != nil {
		return nil, err
	}

	// SubTag 52: LayerAttributes
	attrPayload := BuildLayerAttributesWithTime(layerName, startTime, durationFrames)
	if err := WriteTag(s, TagLayerAttributes, attrPayload); err != nil {
		return nil, err
	}

	// SubTag 13: Transform
	var transPayload []byte
	if scaleX != 1.0 || scaleY != 1.0 {
		transPayload = BuildTransform2D(posX, posY, scaleX, scaleY)
	} else {
		transPayload = BuildDefaultTransform(posX, posY)
	}
	if err := WriteTag(s, TagLayerTransform, transPayload); err != nil {
		return nil, err
	}

	// SubTags for Shapes (ShapeGroup, Fill, Stroke, etc.)
	for _, shapeTag := range shapeTags {
		s.WriteBytes(shapeTag)
	}

	// EndLayer
	if err := WriteTag(s, TagEnd, nil); err != nil {
		return nil, err
	}

	return s.Bytes(), nil
}

// BuildShapeLayerWithTime creates SubTag 5 (LayerTag) for a native vector Shape Layer with custom startTime and duration.
func BuildShapeLayerWithTime(layerID uint32, layerName string, startTime, durationFrames uint64, posX, posY float32, shapeTags [][]byte) ([]byte, error) {
	return BuildShapeLayerWithTransform(layerID, layerName, startTime, durationFrames, posX, posY, 1.0, 1.0, shapeTags)
}

// BuildShapeLayer creates SubTag 5 (LayerTag) for a native vector Shape Layer starting at frame 0.
func BuildShapeLayer(layerID uint32, layerName string, durationFrames uint64, posX, posY float32, shapeTags [][]byte) ([]byte, error) {
	return BuildShapeLayerWithTime(layerID, layerName, 0, durationFrames, posX, posY, shapeTags)
}

// BuildSolidLayer creates SubTag 5 (LayerTag) for a solid color background layer.
func BuildSolidLayer(layerID uint32, layerName string, durationFrames uint64, width, height int32, r, g, b byte) ([]byte, error) {
	s := NewBitStream()
	// LayerType = 1 (SolidLayer)
	if err := s.WriteByte(LayerTypeSolid); err != nil {
		return nil, err
	}
	if err := s.WriteEncodedUint(uint64(layerID)); err != nil {
		return nil, err
	}

	// SubTag 52: LayerAttributes
	attrPayload := BuildLayerAttributes(layerName, durationFrames)
	if err := WriteTag(s, TagLayerAttributes, attrPayload); err != nil {
		return nil, err
	}

	// SubTag 13: Transform
	transPayload := BuildDefaultTransform(0, 0)
	if err := WriteTag(s, TagLayerTransform, transPayload); err != nil {
		return nil, err
	}

	// SubTag 7: SolidColor
	solidStream := NewBitStream()
	solidStream.WriteColor(r, g, b)
	solidStream.WriteEncodedInt(int64(width))
	solidStream.WriteEncodedInt(int64(height))
	if err := WriteTag(s, TagSolidColor, solidStream.Bytes()); err != nil {
		return nil, err
	}

	// EndLayer
	if err := WriteTag(s, TagEnd, nil); err != nil {
		return nil, err
	}

	return s.Bytes(), nil
}

// BuildShapeGroup compiles a group of shape elements into Tag 15 (ShapeGroup).
func BuildShapeGroup(elements [][]byte) ([]byte, error) {
	// Flags for ShapeGroup:
	// blendMode=0, anchorPoint=0, position=0, scale=0, skew=0, skewAxis=0, rotation=0, opacity=0, elements=1
	flags := NewBitStream()
	for i := 0; i < 8; i++ {
		flags.WriteBitBoolean(false)
	}
	flags.WriteBitBoolean(true) // elements exist flag
	flags.AlignWithBytes()

	s := NewBitStream()
	s.WriteBytes(flags.Bytes())

	// Write child elements
	for _, elem := range elements {
		s.WriteBytes(elem)
	}

	// EndShapeGroup
	if err := WriteTag(s, TagEnd, nil); err != nil {
		return nil, err
	}

	out := NewBitStream()
	if err := WriteTag(out, TagShapeGroup, s.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildShapePath creates Tag 19 (ShapePath) payload.
// verbs contains PathVerb constants (PathVerbMove, PathVerbLine, PathVerbCurve11, PathVerbClose).
// coords contains float32 coordinates for each point (X, Y).
func BuildShapePath(verbs []byte, coords []float32) ([]byte, error) {
	if len(verbs) == 0 {
		return nil, errors.New("empty verbs for ShapePath")
	}

	pw := NewBitStream()
	// 1. Number of verbs (LEB128)
	if err := pw.WriteEncodedUint(uint64(len(verbs))); err != nil {
		return nil, err
	}
	// 2. 3 bits per verb
	for _, v := range verbs {
		pw.WriteUBits(uint32(v), 3)
	}
	// 3. Float list coordinates with 0.05 precision
	pw.WriteFloatList(coords, SpatialPrecision)
	pw.AlignWithBytes()

	// Tag 19 flags:
	// exist = true (1 bit)
	// animatable = false (1 bit)
	flags := NewBitStream()
	flags.WriteBitBoolean(true)
	flags.WriteBitBoolean(false)
	flags.AlignWithBytes()

	payload := append(flags.Bytes(), pw.Bytes()...)

	out := NewBitStream()
	if err := WriteTag(out, TagShapePath, payload); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildRectangle creates Tag 16 (Rectangle) payload.
func BuildRectangle(w, h, posX, posY, roundness float32) ([]byte, error) {
	flags := NewBitStream()
	content := NewBitStream()

	// 1. reversed (false)
	flags.WriteBitBoolean(false)

	// 2. roundness
	if roundness > 0 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		content.WriteFloat32(roundness)
	} else {
		flags.WriteBitBoolean(false)
	}

	// 3. size
	flags.WriteBitBoolean(true)
	flags.WriteBitBoolean(false)
	flags.WriteBitBoolean(false)
	content.WriteFloat32(w)
	content.WriteFloat32(h)

	// 4. position
	if posX != 0 || posY != 0 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		flags.WriteBitBoolean(false)
		content.WriteFloat32(posX)
		content.WriteFloat32(posY)
	} else {
		flags.WriteBitBoolean(false)
	}

	flags.AlignWithBytes()
	payload := append(flags.Bytes(), content.Bytes()...)

	out := NewBitStream()
	if err := WriteTag(out, TagRectangle, payload); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildEllipse creates Tag 17 (Ellipse) payload.
func BuildEllipse(w, h, posX, posY float32) ([]byte, error) {
	flags := NewBitStream()
	content := NewBitStream()

	// 1. reversed (false)
	flags.WriteBitBoolean(false)

	// 2. size
	flags.WriteBitBoolean(true)
	flags.WriteBitBoolean(false)
	flags.WriteBitBoolean(false)
	content.WriteFloat32(w)
	content.WriteFloat32(h)

	// 3. position
	if posX != 0 || posY != 0 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		flags.WriteBitBoolean(false)
		content.WriteFloat32(posX)
		content.WriteFloat32(posY)
	} else {
		flags.WriteBitBoolean(false)
	}

	flags.AlignWithBytes()
	payload := append(flags.Bytes(), content.Bytes()...)

	out := NewBitStream()
	if err := WriteTag(out, TagEllipse, payload); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildFill creates Tag 20 (Fill) payload.

func BuildFill(r, g, b byte, opacity byte) ([]byte, error) {
	flags := NewBitStream()
	content := NewBitStream()

	// blendMode (Normal = 0)
	flags.WriteBitBoolean(false)
	// composite (BelowPrevious = 0)
	flags.WriteBitBoolean(false)
	// fillRule (NonZeroWinding = 0)
	flags.WriteBitBoolean(false)

	// color (Red default in libpag, but we write actual color)
	flags.WriteBitBoolean(true)  // exist
	flags.WriteBitBoolean(false) // not animatable
	content.WriteColor(r, g, b)

	// opacity (if 255/Opaque, default is Opaque so flag=0, otherwise flag=1)
	if opacity != 255 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		content.WriteByte(opacity)
	} else {
		flags.WriteBitBoolean(false)
	}

	flags.AlignWithBytes()
	payload := NewBitStream()
	payload.WriteBytes(flags.Bytes())
	payload.WriteBytes(content.Bytes())

	out := NewBitStream()
	if err := WriteTag(out, TagFill, payload.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildStroke creates Tag 21 (Stroke) payload.
func BuildStroke(r, g, b byte, opacity byte, width float32) ([]byte, error) {
	flags := NewBitStream()
	content := NewBitStream()

	// blendMode (0)
	flags.WriteBitBoolean(false)
	// composite (0)
	flags.WriteBitBoolean(false)
	// lineCap (0 = Butt)
	flags.WriteBitBoolean(false)
	// lineJoin (0 = Miter)
	flags.WriteBitBoolean(false)
	// miterLimit (default 4.0f -> 0)
	flags.WriteBitBoolean(false)

	// color
	flags.WriteBitBoolean(true)
	flags.WriteBitBoolean(false)
	content.WriteColor(r, g, b)

	// opacity
	if opacity != 255 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		content.WriteByte(opacity)
	} else {
		flags.WriteBitBoolean(false)
	}

	// strokeWidth (default 2.0f)
	if width != 2.0 {
		flags.WriteBitBoolean(true)
		flags.WriteBitBoolean(false)
		content.WriteFloat32(width)
	} else {
		flags.WriteBitBoolean(false)
	}

	// dashes (none -> 0)
	flags.WriteBitBoolean(false)

	flags.AlignWithBytes()
	payload := NewBitStream()
	payload.WriteBytes(flags.Bytes())
	payload.WriteBytes(content.Bytes())

	out := NewBitStream()
	if err := WriteTag(out, TagStroke, payload.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// BuildVectorComposition compiles Composition ID, CompositionAttributes, and Layers into Tag 2.
func BuildVectorComposition(compID uint32, width, height int32, durationFrames uint64, fps float32, layers [][]byte) ([]byte, error) {
	s := NewBitStream()
	// 1. Composition ID
	if err := s.WriteEncodedUint(uint64(compID)); err != nil {
		return nil, err
	}

	// 2. SubTag 3: CompositionAttributes
	attrsPayload, err := BuildCompositionAttributes(width, height, durationFrames, fps, 0, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed building composition attributes: %w", err)
	}
	if err := WriteTag(s, TagCompositionAttributes, attrsPayload); err != nil {
		return nil, err
	}

	// 3. SubTag 5 for each layer
	for _, layerPayload := range layers {
		if err := WriteTag(s, TagLayer, layerPayload); err != nil {
			return nil, err
		}
	}

	// 4. SubTag 0: EndTag for this composition
	if err := WriteTag(s, TagEnd, nil); err != nil {
		return nil, err
	}

	return s.Bytes(), nil
}
