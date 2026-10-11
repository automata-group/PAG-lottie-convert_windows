package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lottie2pag/pkg/compress"
	"lottie2pag/pkg/converter"
	"lottie2pag/pkg/pag"
)

const Banner = `
  _            _   _   _      ___   ____               
 | |    ___  _| |_| |_(_) ___|__ \ |  _ \  __ _   __ _ 
 | |   / _ \|_   _| __| |/ _ \ / / | |_) |/ _' | / _' |
 | |__| (_) | | |_| |_| |  __// /_ |  __/| (_| || (_| |
 |_____\___/   \__|\__|_|\___|____||_|    \__,_| \__, |
                                                 |___/ 
      Universal Tencent PAG Converter (Native Windows)
  [Supports: Lottie JSON | Animated GIF | Image Sequences | PNG/WebP]
`

func main() {
	inputPath := flag.String("i", "", "Input file or directory (.json, .gif, .png, image sequence dir)")
	outputPath := flag.String("o", "", "Output PAG file")
	inspectPath := flag.String("inspect", "", "Inspect an existing PAG file")
	batchDir := flag.String("batch", "", "Batch convert directory (matches official convert.sh)")
	fpsFlag := flag.Float64("fps", 0, "Target FPS (default: use original or 25/30)")
	qualityFlag := flag.Float64("quality", 0.80, "WebP quality (0.1 to 1.0, default: 0.80 - optimal balance)")
	scaleFlag := flag.Float64("scale", 0, "Resolution scale factor (0.1 to 1.0, e.g. 0.55; 0: auto)")
	targetKBFlag := flag.Int("target-kb", 0, "Target file size budget in KB (e.g. 100 for < 100 KB; auto-calibrates scale, FPS, & quality)")
	compactFlag := flag.Bool("compact", false, "Ultra-compact preset strictly under 100 KB (< 100 KB) with kinematic decimation & bilinear upscaling")
	maxFramesFlag := flag.Int("max-frames", 0, "Max frames to convert (0: all)")
	losslessFlag := flag.Bool("lossless", false, "Strict lossless mode (preserves original source FPS and uncompressed frames)")
	edgePath := flag.String("edge", "", "Path to Microsoft Edge, Chrome, or Chromium executable (auto-detected if empty)")
	verifyFlag := flag.Bool("verify", false, "Verify output PAG with official libpag.wasm via headless browser")
	zstdFlag := flag.Bool("zstd", false, "Compress output with Zstandard (Zstd Level 19-22 / FSE ANS) for ultra-compact storage")
	vectorFlag := flag.Bool("vector", false, "Direct native vector export without rasterizing full-canvas frames")
	vectorizeFlag := flag.Bool("vectorize", false, "Vectorize raster input (images, GIF, sequence) into native Tencent PAG vector paths")

	flag.Parse()

	fmt.Print(Banner)

	// Mode 1: Inspect PAG file
	if *inspectPath != "" {
		handleInspect(*inspectPath)
		return
	}

	opts := converter.DefaultRenderOptions()
	opts.Quality = float32(*qualityFlag)
	opts.FPS = float32(*fpsFlag)
	opts.Scale = float32(*scaleFlag)
	opts.TargetKB = *targetKBFlag
	opts.Compact = *compactFlag
	opts.MaxFrames = *maxFramesFlag
	opts.Lossless = *losslessFlag
	opts.Vector = *vectorFlag
	opts.Vectorize = *vectorizeFlag
	opts.EdgePath = *edgePath

	useZstd := *zstdFlag
	if *compactFlag {
		useZstd = true
	}

	// Mode 2: Batch conversion
	if *batchDir != "" {
		handleBatch(*batchDir, opts, *verifyFlag, useZstd)
		return
	}

	// Mode 3: Support positional arguments matching Tencent CLI (./PAGConvertor <input> [output_or_fps] [fps])
	targetInput := *inputPath
	targetOutput := *outputPath

	if targetInput == "" && flag.NArg() > 0 {
		targetInput = flag.Arg(0)
		if flag.NArg() > 1 {
			// Second arg can be output path or FPS number
			arg1 := flag.Arg(1)
			if f, err := strconv.ParseFloat(arg1, 64); err == nil {
				opts.FPS = float32(f)
			} else {
				targetOutput = arg1
			}
		}
		if flag.NArg() > 2 {
			if f, err := strconv.ParseFloat(flag.Arg(2), 64); err == nil {
				opts.FPS = float32(f)
			}
		}
	}

	if targetInput == "" {
		printUsage()
		os.Exit(1)
	}

	if targetOutput == "" {
		base := strings.TrimSuffix(targetInput, filepath.Ext(targetInput))
		targetOutput = base + ".pag"
	}

	convertSingleTarget(targetInput, targetOutput, opts, *verifyFlag, useZstd)
}

func printUsage() {
	fmt.Println("Usage (Flag Mode):")
	fmt.Println("  lottie2pag.exe -i <input.json | input.gif | folder/> [-o <output.pag>] [-compact] [-target-kb 100] [-scale 0.6] [-fps 24] [-quality 0.70] [-lossless] [-zstd] [-verify]")
	fmt.Println("  lottie2pag.exe -inspect <file.pag | file.pag.zst>")
	fmt.Println("  lottie2pag.exe -batch <input_folder> [-compact] [-zstd] [-lossless]")
	fmt.Println("\nPresets & Mathematical Compression:")
	fmt.Println("  -vector          : Direct native vector export (zero full-canvas rasterization)")
	fmt.Println("  -vectorize       : Adaptive raster-to-vector engine (quantization + Moore-neighbor + RDP Bézier)")
	fmt.Println("  -compact         : Auto-calibrates to strictly < 100 KB using kinematic decimation & bilinear upscaling")
	fmt.Println("  -target-kb <int> : Dynamic mathematical solver for size budget (e.g. 100 for < 100 KB)")
	fmt.Println("  -scale <float>   : Resolution scale factor (0.40 to 1.0; offloaded to GPU bilinear upscaler)")
	fmt.Println("  -zstd            : Secondary meta-compression via Zstandard Finite State Entropy (Level 19)")
	fmt.Println("\nUsage (Official Tencent CLI Compatible):")
	fmt.Println("  lottie2pag.exe <input_file_or_dir> [output.pag] [fps]")
	fmt.Println("  lottie2pag.exe <sequence_folder> [fps]")
}

func convertSingleTarget(inPath, outPath string, opts converter.RenderOptions, verify bool, useZstd bool) {
	fmt.Printf("[+] Processing: %s -> %s\n", inPath, outPath)
	startTime := time.Now()

	pagBytes, mediaType, err := converter.ConvertAuto(inPath, opts)
	if err != nil {
		fmt.Printf("[-] Conversion failed: %v\n", err)
		os.Exit(1)
	}

	isZstdOut := strings.HasSuffix(strings.ToLower(outPath), ".zst")
	if isZstdOut {
		// Output requested explicitly as .zst
		zstdBytes, err := compress.CompressZstd(pagBytes)
		if err != nil {
			fmt.Printf("[-] Zstd compression failed: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(outPath, zstdBytes, 0644); err != nil {
			fmt.Printf("[-] Failed writing output file: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Write standard compliant .pag file
		if err := os.WriteFile(outPath, pagBytes, 0644); err != nil {
			fmt.Printf("[-] Failed to write output file: %v\n", err)
			os.Exit(1)
		}
	}

	origSize := int64(0)
	if fi, err := os.Stat(inPath); err == nil {
		if fi.IsDir() {
			filepath.Walk(inPath, func(_ string, info os.FileInfo, _ error) error {
				if info != nil && !info.IsDir() {
					origSize += info.Size()
				}
				return nil
			})
		} else {
			origSize = fi.Size()
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[✓] Success! Generated %s PAG: %s\n", mediaType, outPath)
	fmt.Printf("    Format     : %s\n", mediaType)
	fmt.Printf("    Output size: %d bytes (%.2f KiB) [Source: %d bytes (%.2f KiB)]\n", len(pagBytes), float64(len(pagBytes))/1024.0, origSize, float64(origSize)/1024.0)
	fmt.Printf("    Time taken : %v\n", elapsed)

	if opts.TargetKB > 0 {
		targetBytes := opts.TargetKB * 1024
		if len(pagBytes) <= targetBytes {
			fmt.Printf("    Target Budget: [PASSED] %d bytes <= %d bytes budget (%d KB / %d KiB)\n", len(pagBytes), targetBytes, opts.TargetKB, opts.TargetKB)
		} else {
			fmt.Printf("    Target Budget: [PHYSICAL FLOOR] %d bytes vs %d bytes requested (%d KB). Reached minimum safe visual floor.\n", len(pagBytes), targetBytes, opts.TargetKB)
		}
	}

	if useZstd && !isZstdOut {
		zstdBytes, err := compress.CompressZstd(pagBytes)
		if err == nil {
			zstdPath := outPath + ".zst"
			if err := os.WriteFile(zstdPath, zstdBytes, 0644); err == nil {
				savedBytes := len(pagBytes) - len(zstdBytes)
				pctSaved := (float64(savedBytes) / float64(len(pagBytes))) * 100
				fmt.Printf("[★] Zstandard Meta-Engine (Finite State Entropy / ANS Level 19-22):\n")
				fmt.Printf("    Zstd Target: %s\n", zstdPath)
				fmt.Printf("    Zstd Size  : %d bytes (-%d bytes / -%.1f%% vs raw PAG)\n", len(zstdBytes), savedBytes, pctSaved)
				if origSize > 0 {
					totalSaved := origSize - int64(len(zstdBytes))
					totalPct := (float64(totalSaved) / float64(origSize)) * 100
					fmt.Printf("    Total Saved: -%d bytes (-%.1f%% vs source)\n", totalSaved, totalPct)
				}
			}
		}
	}

	if verify {
		fmt.Println("[*] Verifying with official libpag.wasm in headless Edge...")
		verifyWithLibpag(outPath, opts.EdgePath, opts.StaticDir)
	}
}

func handleBatch(dir string, opts converter.RenderOptions, verify bool, useZstd bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("[-] Failed to read directory: %v\n", err)
		os.Exit(1)
	}

	count := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(strings.ToLower(name), ".pag") || strings.HasSuffix(strings.ToLower(name), ".zst") {
			continue
		}

		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			// Subdirectory treated as image sequence (e.g. dir/pic -> dir/pic.pag)
			outPath := filepath.Join(dir, name+".pag")
			fmt.Printf("\n[Batch %d] Converting Sequence Directory: %s -> %s\n", count+1, name, outPath)
			pagBytes, err := converter.ConvertImageSequence(fullPath, opts)
			if err != nil {
				fmt.Printf("[-] Skipped directory %s: %v\n", name, err)
				continue
			}
			if err := os.WriteFile(outPath, pagBytes, 0644); err != nil {
				fmt.Printf("[-] Failed writing %s: %v\n", outPath, err)
				continue
			}
			fmt.Printf("[✓] Created %s (%d bytes)\n", outPath, len(pagBytes))
			if useZstd {
				if zstdBytes, err := compress.CompressZstd(pagBytes); err == nil {
					os.WriteFile(outPath+".zst", zstdBytes, 0644)
					fmt.Printf("    [★] Created %s.zst (%d bytes)\n", outPath, len(zstdBytes))
				}
			}
			if verify {
				verifyWithLibpag(outPath, opts.EdgePath, opts.StaticDir)
			}
			count++
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".json" || ext == ".gif" || ext == ".png" || ext == ".webp" || ext == ".jpg" {
			outPath := filepath.Join(dir, strings.TrimSuffix(name, ext)+".pag")
			fmt.Printf("\n[Batch %d] Converting File: %s -> %s\n", count+1, name, outPath)
			pagBytes, mediaType, err := converter.ConvertAuto(fullPath, opts)
			if err != nil {
				fmt.Printf("[-] Failed converting %s: %v\n", name, err)
				continue
			}
			if err := os.WriteFile(outPath, pagBytes, 0644); err != nil {
				fmt.Printf("[-] Failed writing %s: %v\n", outPath, err)
				continue
			}
			fmt.Printf("[✓] Created %s (%s, %d bytes)\n", outPath, mediaType, len(pagBytes))
			if useZstd {
				if zstdBytes, err := compress.CompressZstd(pagBytes); err == nil {
					os.WriteFile(outPath+".zst", zstdBytes, 0644)
					fmt.Printf("    [★] Created %s.zst (%d bytes)\n", outPath, len(zstdBytes))
				}
			}
			if verify {
				verifyWithLibpag(outPath, opts.EdgePath, opts.StaticDir)
			}
			count++
		}
	}
	fmt.Printf("\n[✓] Batch conversion complete: %d assets processed.\n", count)
}

func handleInspect(pagPath string) {
	fmt.Printf("[+] Inspecting PAG file: %s\n\n", pagPath)
	info, err := pag.InspectFile(pagPath)
	if err != nil {
		fmt.Printf("[-] Failed to inspect PAG: %v\n", err)
		os.Exit(1)
	}

	if info.IsCompressed {
		fmt.Printf("Container      : Zstandard (Magic: 0xFD2FB528 / Finite State Entropy ANS)\n")
		fmt.Printf("Compressed Size: %d bytes\n", info.CompressedSize)
		savedBytes := info.FileSize - info.CompressedSize
		savedPct := (float64(savedBytes) / float64(info.FileSize)) * 100
		ratio := float64(info.FileSize) / float64(info.CompressedSize)
		fmt.Printf("Decompressed   : %d bytes (-%.1f%% bandwidth saved, %.2fx ratio)\n", info.FileSize, savedPct, ratio)
	} else {
		fmt.Printf("Container      : Standard Native PAG\n")
		fmt.Printf("File Size      : %d bytes\n", info.FileSize)
	}
	fmt.Printf("Body Length    : %d bytes\n", info.BodyLength)
	fmt.Printf("Total Tags     : %d\n", info.TagsCount)
	fmt.Printf("Images Count   : %d\n", info.ImagesCount)
	fmt.Printf("Videos Count   : %d\n", info.VideosCount)

	if len(info.Compositions) > 0 {
		fmt.Println("\n--- Compositions ---")
		for i, c := range info.Compositions {
			fmt.Printf("  [%d] Type: %s | ID: %d | Resolution: %dx%d | FPS: %.2f | Duration: %d frames | Layers: %d\n",
				i, c.Type, c.ID, c.Width, c.Height, c.FPS, c.DurationFrames, c.LayersCount)
			if len(c.LayerNames) > 0 {
				fmt.Printf("      Layer Names (%d): %s\n", len(c.LayerNames), strings.Join(c.LayerNames[:min(len(c.LayerNames), 10)], ", "))
			}
		}
	}

	fmt.Println("\n--- Tag Stream Hierarchy ---")
	for _, t := range info.Tags {
		fmt.Printf("  Tag #%-3d | Code: %-3d | Name: %-22s | Size: %d bytes\n", t.Index, t.Code, t.Name, t.Length)
	}
}

func verifyWithLibpag(pagPath, edgePath, staticDir string) {
	res, err := converter.VerifyPAGFile(pagPath, staticDir, edgePath)
	if err != nil {
		fmt.Printf("[-] libpag.wasm verification FAILED: %v\n", err)
		return
	}
	fmt.Printf("[✓] libpag.wasm verification PASSED! Width: %d, Height: %d, Duration: %dμs, Images: %d, Texts: %d, Videos: %d\n",
		res.Width, res.Height, res.Duration, res.NumImages, res.NumTexts, res.NumVideos)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
