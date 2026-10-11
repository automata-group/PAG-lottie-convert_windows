package converter

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"lottie2pag/pkg/pag"
)

// naturalSort implements natural human sorting for filenames (e.g. frame_2 before frame_10).
func naturalSort(files []string) {
	sort.Slice(files, func(i, j int) bool {
		return naturalLess(files[i], files[j])
	})
}

func naturalLess(s1, s2 string) bool {
	for len(s1) > 0 && len(s2) > 0 {
		if unicode.IsDigit(rune(s1[0])) && unicode.IsDigit(rune(s2[0])) {
			n1, rem1 := extractNumber(s1)
			n2, rem2 := extractNumber(s2)
			if n1 != n2 {
				return n1 < n2
			}
			s1, s2 = rem1, rem2
		} else {
			c1 := strings.ToLower(string(s1[0]))
			c2 := strings.ToLower(string(s2[0]))
			if c1 != c2 {
				return c1 < c2
			}
			s1, s2 = s1[1:], s2[1:]
		}
	}
	return len(s1) < len(s2)
}

func extractNumber(s string) (int64, string) {
	idx := 0
	for idx < len(s) && unicode.IsDigit(rune(s[idx])) {
		idx++
	}
	n, _ := strconv.ParseInt(s[:idx], 10, 64)
	return n, s[idx:]
}

// ConvertImageSequence converts a directory of image frames into an animated PAG file.
func ConvertImageSequence(dirPath string, opts RenderOptions) ([]byte, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading directory: %w", err)
	}

	var validFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".png" || ext == ".webp" || ext == ".jpg" || ext == ".jpeg" {
			validFiles = append(validFiles, e.Name())
		}
	}

	if len(validFiles) == 0 {
		return nil, fmt.Errorf("no valid image frames (.png, .webp, .jpg) found in directory: %s", dirPath)
	}

	naturalSort(validFiles)

	if opts.MaxFrames > 0 && len(validFiles) > opts.MaxFrames {
		validFiles = validFiles[:opts.MaxFrames]
	}

	firstPath := filepath.Join(dirPath, validFiles[0])
	firstData, err := os.ReadFile(firstPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading first frame %s: %w", firstPath, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(firstData))
	if err != nil {
		return nil, fmt.Errorf("failed decoding dimensions from first frame: %w", err)
	}

	width := int32(cfg.Width)
	height := int32(cfg.Height)
	totalFrames := len(validFiles)

	fps := float32(25.0)
	if opts.FPS > 0 {
		fps = opts.FPS
	}

	container := pag.NewContainer()
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	type encodedFrame struct {
		index    int
		webpData []byte
		err      error
	}

	frameWebP := make([][]byte, totalFrames)
	jobs := make(chan int, totalFrames)
	results := make(chan encodedFrame, totalFrames)

	numWorkers := 8
	if numWorkers > totalFrames {
		numWorkers = totalFrames
	}

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				fname := validFiles[idx]
				fpath := filepath.Join(dirPath, fname)
				data, err := os.ReadFile(fpath)
				if err != nil {
					results <- encodedFrame{index: idx, err: err}
					continue
				}
				ext := strings.ToLower(filepath.Ext(fname))
				if ext == ".webp" {
					results <- encodedFrame{index: idx, webpData: data}
				} else {
					webp, _, _, err := EncodeToWebP(data, opts.Quality)
					results <- encodedFrame{index: idx, webpData: webp, err: err}
				}
			}
		}()
	}

	for i := 0; i < totalFrames; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			return nil, fmt.Errorf("failed encoding frame %s: %w", validFiles[res.index], res.err)
		}
		frameWebP[res.index] = res.webpData
	}

	layers := make([][]byte, 0, totalFrames)

	for idx := 0; idx < totalFrames; idx++ {
		imageID := uint32(idx + 1)
		layerID := uint32(100 + idx)
		fname := validFiles[idx]
		webpData := frameWebP[idx]

		tagPayload, err := pag.BuildImageBytesV3(imageID, webpData, width, height, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building Tag 49 for frame %d: %w", idx, err)
		}
		container.AddTag(pag.TagImageBytesV3, tagPayload)

		layerPayload, err := pag.BuildImageLayerWithTime(layerID, imageID, fname, uint64(idx), 1, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("failed building layer for %s: %w", fname, err)
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
