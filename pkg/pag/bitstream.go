package pag

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

const (
	// SpatialPrecision is Tencent libpag's coordinate quantization unit (0.05f).
	SpatialPrecision float32 = 0.05
	// BezierPrecision is Tencent libpag's bezier control point precision (0.005f).
	BezierPrecision float32 = 0.005
)

// BitStream provides reading and writing utilities for PAG binary format.
// It exactly implements Tencent libpag's EncodeStream / DecodeStream bit-packing,
// variable-length LEB128 integers, sign-magnitude integers, and coordinate packing.
type BitStream struct {
	data       []byte
	bitPos     int
	readBitPos int
}

// NewBitStream creates an empty stream ready for writing.
func NewBitStream() *BitStream {
	return &BitStream{
		data: make([]byte, 0, 256),
	}
}

// NewBitStreamFromBytes creates a stream for reading existing bytes.
func NewBitStreamFromBytes(data []byte) *BitStream {
	return &BitStream{
		data: data,
	}
}

// Bytes returns the accumulated byte slice aligned to byte boundary.
func (s *BitStream) Bytes() []byte {
	s.AlignWithBytes()
	return s.data
}

// Len returns the current length in bytes remaining to be read or written.
func (s *BitStream) Len() int {
	readBytePos := s.readBitPos / 8
	if readBytePos >= len(s.data) {
		return 0
	}
	return len(s.data) - readBytePos
}

// TotalBytes returns the total length of the data slice.
func (s *BitStream) TotalBytes() int {
	return len(s.data)
}

// AlignWithBytes aligns the stream's write position to the next byte boundary.
func (s *BitStream) AlignWithBytes() {
	if s.bitPos%8 != 0 {
		s.bitPos = ((s.bitPos / 8) + 1) * 8
	}
}

// FlushBits is an alias for AlignWithBytes.
func (s *BitStream) FlushBits() {
	s.AlignWithBytes()
}

// WriteByte writes a single byte to the stream.
func (s *BitStream) WriteByte(b byte) error {
	s.AlignWithBytes()
	s.data = append(s.data, b)
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteBytes writes raw byte slice to the stream.
func (s *BitStream) WriteBytes(p []byte) (int, error) {
	s.AlignWithBytes()
	s.data = append(s.data, p...)
	s.bitPos = len(s.data) * 8
	return len(p), nil
}

// WriteUint16 writes an unsigned 16-bit integer in Little-Endian.
func (s *BitStream) WriteUint16(v uint16) error {
	s.AlignWithBytes()
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	s.data = append(s.data, b[:]...)
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteUint32 writes an unsigned 32-bit integer in Little-Endian.
func (s *BitStream) WriteUint32(v uint32) error {
	s.AlignWithBytes()
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	s.data = append(s.data, b[:]...)
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteUint64 writes an unsigned 64-bit integer in Little-Endian.
func (s *BitStream) WriteUint64(v uint64) error {
	s.AlignWithBytes()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	s.data = append(s.data, b[:]...)
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteFloat32 writes an IEEE 754 32-bit float in Little-Endian.
func (s *BitStream) WriteFloat32(f float32) error {
	return s.WriteUint32(math.Float32bits(f))
}

// WriteEncodedUint writes an unsigned integer using standard LEB128 encoding.
func (s *BitStream) WriteEncodedUint(u uint64) error {
	s.AlignWithBytes()
	for {
		b := byte(u & 0x7F)
		u >>= 7
		if u != 0 {
			s.data = append(s.data, b|0x80)
		} else {
			s.data = append(s.data, b)
			break
		}
	}
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteEncodedInt writes a signed integer using Tencent's Sign-Magnitude LEB128 encoding.
// Positive numbers encode as (v << 1), negative as ((-v << 1) | 1).
func (s *BitStream) WriteEncodedInt(v int64) error {
	var u uint64
	if v >= 0 {
		u = uint64(v) << 1
	} else {
		u = (uint64(-v) << 1) | 1
	}
	return s.WriteEncodedUint(u)
}

// WriteTime writes duration/time attribute in frames (unsigned LEB128).
func (s *BitStream) WriteTime(frames uint64) error {
	return s.WriteEncodedUint(frames)
}

// WriteColor writes an RGB color as 3 consecutive bytes (R, G, B).
func (s *BitStream) WriteColor(r, g, b byte) error {
	s.AlignWithBytes()
	s.data = append(s.data, r, g, b)
	s.bitPos = len(s.data) * 8
	return nil
}

// WriteUBits writes the specified number of lower bits of value into the bitstream (LSB-first into byte).
// Matches Tencent libpag EncodeStream::writeUBits.
func (s *BitStream) WriteUBits(value uint32, numBits uint8) {
	for numBits > 0 {
		byteIndex := s.bitPos / 8
		bitOffset := s.bitPos % 8
		if byteIndex >= len(s.data) {
			s.data = append(s.data, 0)
		}
		bitLength := 8 - bitOffset
		if int(numBits) < bitLength {
			bitLength = int(numBits)
		}
		mask := byte((1 << bitLength) - 1)
		bits := byte(value) & mask
		s.data[byteIndex] |= (bits << bitOffset)
		value >>= bitLength
		numBits -= uint8(bitLength)
		s.bitPos += bitLength
	}
}

// WriteBitBoolean writes a 1-bit boolean flag (1 for true, 0 for false).
func (s *BitStream) WriteBitBoolean(val bool) {
	if val {
		s.WriteUBits(1, 1)
	} else {
		s.WriteUBits(0, 1)
	}
}

// WriteBits writes numBits of signed integer into bitstream.
func (s *BitStream) WriteBits(value int32, numBits uint8) {
	data := (uint32(value) << (32 - numBits)) >> (32 - numBits)
	if value < 0 {
		data |= 1 << (numBits - 1)
	}
	s.WriteUBits(data, numBits)
}

// WriteInt32List writes an array of int32 using bit-packed bit width.
// Matches Tencent libpag EncodeStream::writeInt32List.
func (s *BitStream) WriteInt32List(values []int32) {
	if len(values) == 0 {
		s.WriteUBits(0, 5)
		return
	}
	maxBits := uint8(1)
	for _, v := range values {
		bits := bitsForInt32(v)
		if bits > maxBits {
			maxBits = bits
		}
	}
	s.WriteUBits(uint32(maxBits-1), 5)
	for _, v := range values {
		s.WriteBits(v, maxBits)
	}
}

// WriteUint32List writes an array of uint32 using bit-packed bit width.
// Matches Tencent libpag EncodeStream::writeUint32List.
func (s *BitStream) WriteUint32List(values []uint32) {
	if len(values) == 0 {
		s.WriteUBits(0, 5)
		return
	}
	maxBits := uint8(1)
	for _, v := range values {
		bits := bitsForUint32(v)
		if bits > maxBits {
			maxBits = bits
		}
	}
	s.WriteUBits(uint32(maxBits-1), 5)
	for _, v := range values {
		s.WriteUBits(v, maxBits)
	}
}

func bitsForUint32(v uint32) uint8 {
	if v == 0 {
		return 1
	}
	bits := uint8(0)
	for v > 0 {
		bits++
		v >>= 1
	}
	return bits
}


// WriteFloatList quantizes float32 values with given precision and writes them.
// Matches Tencent libpag EncodeStream::writeFloatList.
func (s *BitStream) WriteFloatList(values []float32, precision float32) {
	if len(values) == 0 {
		s.WriteUBits(0, 5)
		return
	}
	scale := 1.0 / precision
	ints := make([]int32, len(values))
	for i, v := range values {
		ints[i] = int32(math.Round(float64(v * scale)))
	}
	s.WriteInt32List(ints)
}

func bitsForInt32(v int32) uint8 {
	if v == 0 || v == -1 {
		return 1
	}
	var u uint32
	if v < 0 {
		u = uint32(^v)
	} else {
		u = uint32(v)
	}
	bits := uint8(1)
	for u > 0 {
		bits++
		u >>= 1
	}
	return bits
}


// WriteUTF8String writes a null-terminated UTF-8 string.
func (s *BitStream) WriteUTF8String(str string) error {
	s.AlignWithBytes()
	s.data = append(s.data, []byte(str)...)
	s.data = append(s.data, 0x00)
	s.bitPos = len(s.data) * 8
	return nil
}

// Read implements io.Reader for BitStream.
func (s *BitStream) Read(p []byte) (n int, err error) {
	byteIndex := s.readBitPos / 8
	if byteIndex >= len(s.data) {
		return 0, io.EOF
	}
	n = copy(p, s.data[byteIndex:])
	s.readBitPos = (byteIndex + n) * 8
	return n, nil
}

// ReadByte reads a single byte from the stream.
func (s *BitStream) ReadByte() (byte, error) {
	byteIndex := s.readBitPos / 8
	if byteIndex >= len(s.data) {
		return 0, io.EOF
	}
	b := s.data[byteIndex]
	s.readBitPos = (byteIndex + 1) * 8
	return b, nil
}

// ReadBytes reads n bytes from the stream.
func (s *BitStream) ReadBytes(n int) ([]byte, error) {
	byteIndex := s.readBitPos / 8
	if byteIndex+n > len(s.data) {
		return nil, io.EOF
	}
	res := make([]byte, n)
	copy(res, s.data[byteIndex:byteIndex+n])
	s.readBitPos = (byteIndex + n) * 8
	return res, nil
}

// ReadUint16 reads a 16-bit unsigned integer in Little-Endian.
func (s *BitStream) ReadUint16() (uint16, error) {
	byteIndex := s.readBitPos / 8
	if byteIndex+2 > len(s.data) {
		return 0, io.EOF
	}
	v := binary.LittleEndian.Uint16(s.data[byteIndex : byteIndex+2])
	s.readBitPos = (byteIndex + 2) * 8
	return v, nil
}

// ReadUint32 reads a 32-bit unsigned integer in Little-Endian.
func (s *BitStream) ReadUint32() (uint32, error) {
	byteIndex := s.readBitPos / 8
	if byteIndex+4 > len(s.data) {
		return 0, io.EOF
	}
	v := binary.LittleEndian.Uint32(s.data[byteIndex : byteIndex+4])
	s.readBitPos = (byteIndex + 4) * 8
	return v, nil
}

// ReadFloat32 reads a 32-bit float in Little-Endian.
func (s *BitStream) ReadFloat32() (float32, error) {
	u, err := s.ReadUint32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(u), nil
}

// ReadEncodedUint reads an unsigned LEB128 integer.
func (s *BitStream) ReadEncodedUint() (uint64, error) {
	var res uint64
	var shift uint
	for {
		b, err := s.ReadByte()
		if err != nil {
			return 0, err
		}
		res |= uint64(b&0x7F) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
		if shift >= 64 {
			return 0, errors.New("LEB128 integer overflow")
		}
	}
	return res, nil
}

// ReadEncodedInt reads a signed integer using Tencent's Sign-Magnitude LEB128 decoding.
func (s *BitStream) ReadEncodedInt() (int64, error) {
	u, err := s.ReadEncodedUint()
	if err != nil {
		return 0, err
	}
	abs := int64(u >> 1)
	if (u & 1) != 0 {
		return -abs, nil
	}
	return abs, nil
}

// ReadTime reads duration/time attribute in frames.
func (s *BitStream) ReadTime() (uint64, error) {
	return s.ReadEncodedUint()
}

// ReadUBits reads numBits unsigned bits (LSB-first).
// Matches Tencent libpag DecodeStream::readUBits.
func (s *BitStream) ReadUBits(numBits uint8) (uint32, error) {
	var value uint32
	var pos uint8
	for pos < numBits {
		byteIndex := s.readBitPos / 8
		bitOffset := s.readBitPos % 8
		if byteIndex >= len(s.data) {
			return 0, io.EOF
		}
		bitLength := 8 - bitOffset
		if int(numBits-pos) < bitLength {
			bitLength = int(numBits - pos)
		}
		mask := byte((1 << bitLength) - 1)
		b := (s.data[byteIndex] >> bitOffset) & mask
		value |= uint32(b) << pos
		pos += uint8(bitLength)
		s.readBitPos += bitLength
	}
	return value, nil
}

// ReadBitBoolean reads a 1-bit boolean flag.
func (s *BitStream) ReadBitBoolean() (bool, error) {
	u, err := s.ReadUBits(1)
	if err != nil {
		return false, err
	}
	return u == 1, nil
}

// ReadBits reads numBits signed integer.
func (s *BitStream) ReadBits(numBits uint8) (int32, error) {
	u, err := s.ReadUBits(numBits)
	if err != nil {
		return 0, err
	}
	signShift := 32 - numBits
	return int32(u<<signShift) >> signShift, nil
}

// ReadUTF8String reads a null-terminated UTF-8 string.
func (s *BitStream) ReadUTF8String() (string, error) {
	start := s.readBitPos / 8
	for i := start; i < len(s.data); i++ {
		if s.data[i] == 0x00 {
			s.readBitPos = (i + 1) * 8
			return string(s.data[start:i]), nil
		}
	}
	return "", io.EOF
}
