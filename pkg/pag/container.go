package pag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Magic bytes at start of every PAG file: 'P', 'A', 'G', 0x01
var PAGMagic = []byte{'P', 'A', 'G', 0x01}

// MarkerByte is 0x55, present immediately after body_length in all standard PAG files.
const MarkerByte byte = 0x55

// Container represents an in-memory PAG file.
type Container struct {
	Tags []Tag
}

// NewContainer initializes an empty PAG container.
func NewContainer() *Container {
	return &Container{
		Tags: make([]Tag, 0),
	}
}

// AddTag adds a tag with given code and payload to the container.
func (c *Container) AddTag(code uint16, payload []byte) {
	c.Tags = append(c.Tags, Tag{
		Code:    code,
		Payload: payload,
	})
}

// Encode serializes the entire PAG file into binary format.
func (c *Container) Encode() ([]byte, error) {
	// First, build the tag stream
	tagStream := NewBitStream()

	for _, t := range c.Tags {
		if err := WriteTag(tagStream, t.Code, t.Payload); err != nil {
			return nil, fmt.Errorf("failed writing tag %d: %w", t.Code, err)
		}
	}

	// Always append EndTag (0x00, 0x00)
	if err := WriteTag(tagStream, TagEnd, nil); err != nil {
		return nil, fmt.Errorf("failed writing end tag: %w", err)
	}

	tagBytes := tagStream.Bytes()
	bodyLen := uint32(len(tagBytes))

	// Final file buffer: Magic (4) + bodyLen (4) + Marker (1) + tagBytes
	out := bytes.NewBuffer(make([]byte, 0, 9+bodyLen))

	// 1. Magic
	out.Write(PAGMagic)

	// 2. Body Length (Little-Endian uint32)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], bodyLen)
	out.Write(lenBuf[:])

	// 3. Marker Byte (0x55)
	out.WriteByte(MarkerByte)

	// 4. Tag Stream
	out.Write(tagBytes)

	return out.Bytes(), nil
}

// WriteFile writes the encoded PAG binary to a disk path.
func (c *Container) WriteFile(filePath string) error {
	data, err := c.Encode()
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// Decode parses a PAG file buffer into Container and its tags.
func Decode(data []byte) (*Container, error) {
	if len(data) < 9 {
		return nil, errors.New("data too short to be a valid PAG file")
	}

	// Verify magic
	if !bytes.Equal(data[:4], PAGMagic) {
		return nil, fmt.Errorf("invalid PAG magic: %q", string(data[:4]))
	}

	bodyLen := binary.LittleEndian.Uint32(data[4:8])
	marker := data[8]
	if marker != MarkerByte {
		return nil, fmt.Errorf("unexpected marker byte: 0x%02x (expected 0x55)", marker)
	}

	if uint32(len(data)-9) < bodyLen {
		return nil, fmt.Errorf("truncated PAG file: expected %d body bytes, got %d", bodyLen, len(data)-9)
	}

	container := NewContainer()
	stream := NewBitStreamFromBytes(data[9 : 9+bodyLen])

	for stream.Len() > 0 {
		code, length, err := ReadTagHeader(stream)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("error reading tag header: %w", err)
		}

		if code == TagEnd {
			break
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(stream, payload); err != nil {
			return nil, fmt.Errorf("error reading tag %d payload: %w", code, err)
		}

		container.AddTag(code, payload)
	}

	return container, nil
}
