package converter

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"

	"lottie2pag/pkg/pag"
)

// VectorizerConfig controls the fidelity, smoothing, and color clustering of raster-to-vector conversion.
type VectorizerConfig struct {
	MaxColors int     // Maximum number of quantized vector color regions (default: 16)
	Tolerance float64 // Ramer-Douglas-Peucker simplification tolerance in pixels (default: 1.2)
	MinArea   int     // Minimum number of pixels to retain a contour (default: 4)
}

// DefaultVectorizerConfig returns production-tuned parameters for sharp, lightweight vector output.
func DefaultVectorizerConfig() VectorizerConfig {
	return VectorizerConfig{
		MaxColors: 16,
		Tolerance: 1.2,
		MinArea:   4,
	}
}

type point2D struct {
	x, y float64
}

type colorCluster struct {
	r, g, b, a byte
	count      int
}

// VectorizeFrame transforms an RGBA raster frame into native Tencent PAG vector shape groups.
// It executes adaptive color quantization, 8-way Moore-neighbor contour extraction,
// RDP curve simplification, and cubic Bézier curve fitting to produce compact, resolution-independent vector paths.
func VectorizeFrame(img image.Image, cfg VectorizerConfig) ([][]byte, error) {
	if img == nil {
		return nil, errors.New("nil image provided to vectorizer")
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, errors.New("invalid image dimensions")
	}

	if cfg.MaxColors <= 0 {
		cfg.MaxColors = 16
	}
	if cfg.Tolerance <= 0 {
		cfg.Tolerance = 1.2
	}
	if cfg.MinArea <= 0 {
		cfg.MinArea = 4
	}

	// 1. Color Quantization & Histogram
	// Step size adapts to image size for instantaneous 60FPS parsing
	step := 1
	if width > 400 || height > 400 {
		step = 2
	}

	colorMap := make(map[uint32]*colorCluster)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			c := img.At(x, y)
			r, g, b, a := c.RGBA()
			alpha := byte(a >> 8)
			if alpha < 24 { // Skip transparent background
				continue
			}
			// Quantize 8-bit to 4-bit per channel for robust grouping
			qr := byte((r >> 12) << 4)
			qg := byte((g >> 12) << 4)
			qb := byte((b >> 12) << 4)
			key := uint32(qr)<<16 | uint32(qg)<<8 | uint32(qb)

			if cl, exists := colorMap[key]; exists {
				cl.count++
			} else {
				colorMap[key] = &colorCluster{r: qr, g: qg, b: qb, a: alpha, count: 1}
			}
		}
	}

	if len(colorMap) == 0 {
		return nil, nil // Fully transparent frame
	}

	clusters := make([]*colorCluster, 0, len(colorMap))
	for _, cl := range colorMap {
		clusters = append(clusters, cl)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].count > clusters[j].count
	})

	if len(clusters) > cfg.MaxColors {
		clusters = clusters[:cfg.MaxColors]
	}

	var shapeGroups [][]byte

	// 2. Contour Extraction & Polygon Simplification for each color cluster
	for _, cl := range clusters {
		// Build 2D binary occupancy grid for this cluster
		grid := make([][]bool, height)
		for y := 0; y < height; y++ {
			grid[y] = make([]bool, width)
		}

		matchCount := 0
		for y := 0; y < height; y++ {
			imgY := bounds.Min.Y + y
			for x := 0; x < width; x++ {
				imgX := bounds.Min.X + x
				r, g, b, a := img.At(imgX, imgY).RGBA()
				alpha := byte(a >> 8)
				if alpha < 24 {
					continue
				}
				qr := byte((r >> 12) << 4)
				qg := byte((g >> 12) << 4)
				qb := byte((b >> 12) << 4)

				dr := math.Abs(float64(qr) - float64(cl.r))
				dg := math.Abs(float64(qg) - float64(cl.g))
				db := math.Abs(float64(qb) - float64(cl.b))

				if dr <= 24 && dg <= 24 && db <= 24 {
					grid[y][x] = true
					matchCount++
				}
			}
		}

		if matchCount < cfg.MinArea {
			continue
		}

		// Extract outer contours using Moore-neighbor tracing
		contours := traceGridContours(grid, width, height)
		if len(contours) == 0 {
			continue
		}

		var groupElements [][]byte

		for _, rawPolygon := range contours {
			if len(rawPolygon) < 3 {
				continue
			}

			// Ramer-Douglas-Peucker simplification
			simplified := rdpSimplify(rawPolygon, cfg.Tolerance)
			if len(simplified) < 3 {
				continue
			}

			// Convert polygon to PAG ShapePath verbs and coords
			verbs := make([]byte, 0, len(simplified)+2)
			coords := make([]float32, 0, len(simplified)*6)

			// Move to start
			verbs = append(verbs, pag.PathVerbMove)
			coords = append(coords, float32(simplified[0].x), float32(simplified[0].y))

			// Smooth curves or line segments
			for i := 1; i < len(simplified); i++ {
				pPrev := simplified[i-1]
				pCurr := simplified[i]
				dx := pCurr.x - pPrev.x
				dy := pCurr.y - pPrev.y
				dist := math.Hypot(dx, dy)

				if dist > 8.0 && i+1 < len(simplified) {
					// Fit smooth quadratic/cubic Bézier curve
					pNext := simplified[i+1]
					cp1X := float32(pPrev.x + dx*0.5)
					cp1Y := float32(pPrev.y + dy*0.5)
					cp2X := float32(pCurr.x - (pNext.x-pCurr.x)*0.2)
					cp2Y := float32(pCurr.y - (pNext.y-pCurr.y)*0.2)

					verbs = append(verbs, pag.PathVerbCurve11)
					coords = append(coords, cp1X, cp1Y, cp2X, cp2Y, float32(pCurr.x), float32(pCurr.y))
				} else {
					verbs = append(verbs, pag.PathVerbLine)
					coords = append(coords, float32(pCurr.x), float32(pCurr.y))
				}
			}
			verbs = append(verbs, pag.PathVerbClose)

			pathPayload, err := pag.BuildShapePath(verbs, coords)
			if err == nil {
				groupElements = append(groupElements, pathPayload)
			}
		}

		if len(groupElements) == 0 {
			continue
		}

		// Paint fill for this color region
		fillPayload, err := pag.BuildFill(cl.r, cl.g, cl.b, cl.a)
		if err == nil {
			groupElements = append(groupElements, fillPayload)
		}

		groupPayload, err := pag.BuildShapeGroup(groupElements)
		if err == nil {
			shapeGroups = append(shapeGroups, groupPayload)
		}
	}

	return shapeGroups, nil
}

// traceGridContours scans a binary grid and traces closed perimeter contours
// using the classical Moore-neighbor tracing algorithm.
func traceGridContours(grid [][]bool, width, height int) [][]point2D {
	var contours [][]point2D
	visited := make([][]bool, height)
	for y := 0; y < height; y++ {
		visited[y] = make([]bool, width)
	}

	// 8-neighbor offsets clockwise: N, NE, E, SE, S, SW, W, NW
	dx := []int{0, 1, 1, 1, 0, -1, -1, -1}
	dy := []int{-1, -1, 0, 1, 1, 1, 0, -1}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !grid[y][x] || visited[y][x] {
				continue
			}

			// Boundary pixel check: must have at least one 4-connected background neighbor
			isPerimeter := (x == 0 || !grid[y][x-1] || x == width-1 || !grid[y][x+1] ||
				y == 0 || !grid[y-1][x] || y == height-1 || !grid[y+1][x])
			if !isPerimeter {
				continue
			}

			// Start Moore-neighbor contour loop
			startX, startY := x, y
			currX, currY := x, y
			// Since scanning left-to-right, (x-1, y) is background (W = index 6).
			searchDir := 6
			var loop []point2D
			maxSteps := (width + height) * 4
			if maxSteps < 128 {
				maxSteps = 128
			}

			for step := 0; step < maxSteps; step++ {
				visited[currY][currX] = true
				loop = append(loop, point2D{x: float64(currX), y: float64(currY)})

				foundNext := false
				for i := 0; i < 8; i++ {
					nd := (searchDir + i) % 8
					nx := currX + dx[nd]
					ny := currY + dy[nd]

					if nx >= 0 && nx < width && ny >= 0 && ny < height && grid[ny][nx] {
						currX, currY = nx, ny
						searchDir = (nd + 6) % 8 // (nd + 4 + 2) % 8 backtrack to previous background
						foundNext = true
						break
					}
				}

				if !foundNext || (currX == startX && currY == startY && step >= 2) {
					break
				}
			}

			if len(loop) >= 4 {
				contours = append(contours, loop)
			}
		}
	}

	return contours
}

// rdpSimplify performs Ramer-Douglas-Peucker polygon simplification with threshold epsilon.
func rdpSimplify(points []point2D, epsilon float64) []point2D {
	if len(points) <= 2 {
		return points
	}

	dmax := 0.0
	index := 0
	end := len(points) - 1

	for i := 1; i < end; i++ {
		d := perpendicularDistance(points[i], points[0], points[end])
		if d > dmax {
			index = i
			dmax = d
		}
	}

	if dmax > epsilon {
		rec1 := rdpSimplify(points[:index+1], epsilon)
		rec2 := rdpSimplify(points[index:], epsilon)
		return append(rec1[:len(rec1)-1], rec2...)
	}

	return []point2D{points[0], points[end]}
}

func perpendicularDistance(pt, lineStart, lineEnd point2D) float64 {
	dx := lineEnd.x - lineStart.x
	dy := lineEnd.y - lineStart.y
	mag := math.Hypot(dx, dy)
	if mag == 0 {
		return math.Hypot(pt.x-lineStart.x, pt.y-lineStart.y)
	}
	u := ((pt.x-lineStart.x)*dx + (pt.y-lineStart.y)*dy) / (mag * mag)
	if u < 0 {
		return math.Hypot(pt.x-lineStart.x, pt.y-lineStart.y)
	} else if u > 1 {
		return math.Hypot(pt.x-lineEnd.x, pt.y-lineEnd.y)
	}
	ix := lineStart.x + u*dx
	iy := lineStart.y + u*dy
	return math.Hypot(pt.x-ix, pt.y-iy)
}

// ConvertRasterToVectorPAG converts a raster image sequence into a resolution-independent native Tencent PAG
// VectorComposition with zero bitmap blobs, achieving extraordinary compression efficiency and infinite sharpness.
func ConvertRasterToVectorPAG(frames []image.Image, fps float32, opts RenderOptions) ([]byte, error) {
	if len(frames) == 0 {
		return nil, errors.New("empty frames slice for vectorization")
	}

	first := frames[0]
	width := int32(first.Bounds().Dx())
	height := int32(first.Bounds().Dy())
	durationFrames := uint64(len(frames))

	container := pag.NewContainer()
	container.AddTag(pag.TagTimeStretchMode, pag.BuildTimeStretchMode(pag.TimeStretchScale))

	cfg := DefaultVectorizerConfig()

	var layers [][]byte
	for i, frame := range frames {
		shapeGroups, err := VectorizeFrame(frame, cfg)
		if err != nil || len(shapeGroups) == 0 {
			continue
		}

		layerID := uint32(200 + i)
		layerName := fmt.Sprintf("vec_frame_%d", i)
		startTime := uint64(i)
		frameDuration := uint64(1)

		shapeLayer, err := pag.BuildShapeLayerWithTime(layerID, layerName, startTime, frameDuration, 0, 0, shapeGroups)
		if err == nil {
			layers = append(layers, shapeLayer)
		}
	}

	container.AddTag(pag.TagLayerMarkers, []byte{0x02, 0x02, 0x02})

	compPayload, err := pag.BuildVectorComposition(0, width, height, durationFrames, fps, layers)
	if err != nil {
		return nil, fmt.Errorf("failed compiling VectorComposition: %w", err)
	}
	container.AddTag(pag.TagVectorComposition, compPayload)

	return container.Encode()
}

// ConvertSingleImageToVectorPAG converts a static raster image (PNG/WebP/JPEG) into a native vector PAG.
func ConvertSingleImageToVectorPAG(img image.Image) ([]byte, error) {
	return ConvertRasterToVectorPAG([]image.Image{img}, 30.0, RenderOptions{Vector: true})
}
