package converter

import (
	"bytes"
	"fmt"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"

	"lottie2pag/pkg/pag"
)

// ConvertGIF converts an animated GIF file into a native PAG animation.
func ConvertGIF(gifPath string, opts RenderOptions) ([]byte, error) {
	file, err := os.Open(gifPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening GIF file: %w", err)
	}
	defer file.Close()

	g, err := gif.DecodeAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed decoding GIF: %w", err)
	}

	if len(g.Image) == 0 {
		return nil, fmt.Errorf("empty GIF with no frames: %s", gifPath)
	}

	width := int32(g.Config.Width)
	height := int32(g.Config.Height)
	if width <= 0 || height <= 0 {
		width = int32(g.Image[0].Bounds().Dx())
		height = int32(g.Image[0].Bounds().Dy())
	}

	totalFrames := len(g.Image)
	if opts.MaxFrames > 0 && totalFrames > opts.MaxFrames {
		totalFrames = opts.MaxFrames
	}

	// Calculate FPS from GIF frame delays
	totalDelay100ths := 0
	for i := 0; i < totalFrames; i++ {
		d := 10 // default 0.10s
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = g.Delay[i]
		}
		totalDelay100ths += d
	}

	fps := float32(25.0)
	if totalDelay100ths > 0 {
		avgDelaySeconds := (float64(totalDelay100ths) / float64(totalFrames)) / 100.0
		if avgDelaySeconds > 0 {
			fps = float32(1.0 / avgDelaySeconds)
		}
	}
	if opts.FPS > 0 {
		fps = opts.FPS
	}
	if fps <= 0 || fps > 120 {
		fps = 25.0
	}

	container := pag.NewContainer()
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	layers := make([][]byte, 0, totalFrames)

	for idx := 0; idx < totalFrames; idx++ {
		img := g.Image[idx]
		imageID := uint32(idx + 1)
		layerID := uint32(100 + idx)

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("failed encoding frame %d to PNG: %w", idx, err)
		}

		webpData, _, _, err := EncodeToWebP(buf.Bytes(), opts.Quality)
		if err != nil {
			return nil, fmt.Errorf("failed encoding frame %d to WebP: %w", idx, err)
		}

		tagPayload, err := pag.BuildImageBytesV3(imageID, webpData, width, height, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building Tag 49 for frame %d: %w", idx, err)
		}
		container.AddTag(pag.TagImageBytesV3, tagPayload)

		frameName := fmt.Sprintf("%s_frame_%04d.webp", filepath.Base(gifPath), idx)
		layerPayload, err := pag.BuildImageLayerWithTime(layerID, imageID, frameName, uint64(idx), 1, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building layer %d: %w", idx, err)
		}
		layers = append(layers, layerPayload)
	}

	container.AddTag(pag.TagImageScaleModes, []byte{0x02, 0x02, 0x02})

	compPayload, err := pag.BuildVectorComposition(0, width, height, uint64(totalFrames), fps, layers)
	if err != nil {
		return nil, fmt.Errorf("failed building composition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}
