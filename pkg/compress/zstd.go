package compress

import (
	"bytes"
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"
)

// ZstdMagic is the 4-byte standard magic header of a Zstandard frame (0xFD2FB528).
var ZstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// IsZstd checks if data begins with the official Zstandard magic header.
func IsZstd(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return bytes.Equal(data[:4], ZstdMagic)
}

// CompressZstd compresses raw data using the state-of-the-art Zstandard (Zstd) algorithm
// at the highest compression level (SpeedBestCompression, Level 19-22) utilizing
// Finite State Entropy (FSE) based on Asymmetric Numeral Systems (tANS).
func CompressZstd(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return nil, err
	}
	if _, err := enc.Write(data); err != nil {
		enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecompressZstd decompresses a Zstandard frame into its original uncompressed bytes.
func DecompressZstd(data []byte) ([]byte, error) {
	if !IsZstd(data) {
		return nil, errors.New("data does not have a valid Zstandard magic header")
	}
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	return io.ReadAll(dec)
}
