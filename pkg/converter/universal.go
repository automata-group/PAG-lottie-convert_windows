package converter

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"lottie2pag/pkg/lottie"
)

// MediaType represents the detected input format.
type MediaType string

const (
	MediaTypeLottie   MediaType = "Lottie JSON"
	MediaTypeGIF      MediaType = "Animated GIF"
	MediaTypeSequence MediaType = "Image Sequence"
	MediaTypeImage    MediaType = "Static Image"
	MediaTypeUnknown  MediaType = "Unknown"
)

// ConvertAuto automatically detects input format and converts to native PAG.
func ConvertAuto(inputPath string, opts RenderOptions) ([]byte, MediaType, error) {
	fi, err := os.Stat(inputPath)
	if err != nil {
		return nil, MediaTypeUnknown, fmt.Errorf("input path not accessible: %w", err)
	}

	// 1. Directory -> Image Sequence
	if fi.IsDir() {
		pagBytes, err := ConvertImageSequence(inputPath, opts)
		if err != nil {
			return nil, MediaTypeSequence, err
		}
		return pagBytes, MediaTypeSequence, nil
	}

	// 2. File -> dispatch by extension
	ext := strings.ToLower(filepath.Ext(inputPath))
	switch ext {
	case ".json", ".lottie":
		anim, err := lottie.ParseFile(inputPath)
		if err != nil {
			return nil, MediaTypeLottie, fmt.Errorf("failed parsing Lottie JSON: %w", err)
		}
		var pagBytes []byte
		if opts.Hybrid || opts.Vector || opts.Vectorize {
			pagBytes, err = ConvertLottieHybrid(anim, opts)
		} else {
			pagBytes, err = ConvertLottieToPAG(anim, opts)
		}
		if err != nil {
			return nil, MediaTypeLottie, err
		}
		return pagBytes, MediaTypeLottie, nil

	case ".gif":
		pagBytes, err := ConvertGIF(inputPath, opts)
		if err != nil {
			return nil, MediaTypeGIF, err
		}
		return pagBytes, MediaTypeGIF, nil

	case ".png", ".webp", ".jpg", ".jpeg":
		if opts.Vector || opts.Vectorize {
			file, err := os.Open(inputPath)
			if err != nil {
				return nil, MediaTypeImage, fmt.Errorf("failed opening image: %w", err)
			}
			defer file.Close()
			img, _, err := image.Decode(file)
			if err != nil {
				return nil, MediaTypeImage, fmt.Errorf("failed decoding image: %w", err)
			}
			pagBytes, err := ConvertSingleImageToVectorPAG(img)
			if err != nil {
				return nil, MediaTypeImage, fmt.Errorf("failed vectorizing image: %w", err)
			}
			return pagBytes, MediaTypeImage, nil
		}
		pagBytes, err := ConvertSingleImage(inputPath, opts)
		if err != nil {
			return nil, MediaTypeImage, err
		}
		return pagBytes, MediaTypeImage, nil

	default:
		return nil, MediaTypeUnknown, fmt.Errorf("unsupported input format '%s'. Supported: .json, .gif, .png, .webp, .jpg, or image sequence directory", ext)
	}
}
