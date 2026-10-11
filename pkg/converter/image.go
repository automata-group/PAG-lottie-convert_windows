package converter

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"lottie2pag/pkg/pag"
)

// EncodeToWebP converts any image data (PNG/JPG) to WebP bytes using PIL or fallback.
func EncodeToWebP(imgData []byte, quality float32) ([]byte, int32, int32, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(imgData))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed decoding image header: %w", err)
	}

	w, h := int32(cfg.Width), int32(cfg.Height)

	q := int(quality * 100)
	if q <= 0 || q > 100 {
		q = 90
	}

	cmd := exec.Command("python", "-c", fmt.Sprintf(`
import sys, io
from PIL import Image
data = sys.stdin.buffer.read()
im = Image.open(io.BytesIO(data))
if im.mode not in ('RGBA', 'RGB'):
    im = im.convert('RGBA')
buf = io.BytesIO()
im.save(buf, format='WEBP', quality=%d)
sys.stdout.buffer.write(buf.getvalue())
`, q))

	cmd.Stdin = bytes.NewReader(imgData)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, 0, 0, fmt.Errorf("webp conversion failed: %v (%s)", err, stderr.String())
	}

	return out.Bytes(), w, h, nil
}

// ConvertSingleImage converts a static image (.png, .webp, .jpg) into a native PAG file.
func ConvertSingleImage(imgPath string, opts RenderOptions) ([]byte, error) {
	data, err := os.ReadFile(imgPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading image: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(imgPath))
	var (
		webpBytes []byte
		w, h      int32
	)

	if ext == ".webp" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed decoding webp config: %w", err)
		}
		webpBytes = data
		w, h = int32(cfg.Width), int32(cfg.Height)
	} else {
		webpBytes, w, h, err = EncodeToWebP(data, opts.Quality)
		if err != nil {
			return nil, err
		}
	}

	imageID := uint32(1)
	container := pag.NewContainer()

	// Tag 32: TimeStretchMode
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	// Tag 49: ImageBytesV3 (WebP)
	tag49, err := pag.BuildImageBytesV3(imageID, webpBytes, w, h, 0, 0)
	if err != nil {
		return nil, err
	}
	container.AddTag(pag.TagImageBytesV3, tag49)

	// Layer referencing image
	layerName := filepath.Base(imgPath)
	layerPayload, err := pag.BuildImageLayer(101, imageID, layerName, 1, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed building layer: %w", err)
	}

	// Tag 94: ImageScaleModes
	container.AddTag(pag.TagImageScaleModes, []byte{0x02, 0x02, 0x02})

	fps := float32(30.0)
	if opts.FPS > 0 {
		fps = opts.FPS
	}

	compPayload, err := pag.BuildVectorComposition(0, w, h, 1, fps, [][]byte{layerPayload})
	if err != nil {
		return nil, fmt.Errorf("failed building composition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}
