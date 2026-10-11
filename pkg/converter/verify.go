package converter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"lottie2pag/pkg/compress"
)

// VerificationResult contains the detailed inspection returned by libpag.wasm.
type VerificationResult struct {
	Success   bool   `json:"success"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  int64  `json:"duration"`
	NumImages int    `json:"numImages"`
	NumTexts  int    `json:"numTexts"`
	NumVideos int    `json:"numVideos"`
	Error     string `json:"error"`
}

// VerifyPAGFile verifies a PAG file using official libpag.wasm inside headless Edge (Pure Go harness).
func VerifyPAGFile(pagPath string, staticDir string, edgePath string) (*VerificationResult, error) {
	pagBytes, err := os.ReadFile(pagPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading PAG file: %w", err)
	}

	return VerifyPAGData(pagBytes, staticDir, edgePath)
}

// VerifyPAGData verifies raw PAG bytes against libpag.wasm in headless Microsoft Edge.
func VerifyPAGData(pagBytes []byte, staticDir string, edgePath string) (*VerificationResult, error) {
	if compress.IsZstd(pagBytes) {
		decomp, err := compress.DecompressZstd(pagBytes)
		if err != nil {
			return nil, fmt.Errorf("failed decompressing Zstandard PAG stream: %w", err)
		}
		pagBytes = decomp
	}

	browserPath, err := FindBrowserExecutable(edgePath)
	if err != nil {
		return nil, fmt.Errorf("headless browser discovery failed: %w", err)
	}
	edgePath = browserPath
	resolvedStatic := ResolveStaticDir(staticDir)

	wasmPath := filepath.Join(resolvedStatic, "libpag.wasm")
	esmPath := filepath.Join(resolvedStatic, "libpag.esm.js")

	if _, err := os.Stat(wasmPath); err != nil {
		return nil, fmt.Errorf("libpag.wasm not found in static directory (%s); to audit with official WASM, place libpag.wasm and libpag.esm.js in ./static", resolvedStatic)
	}
	if _, err := os.Stat(esmPath); err != nil {
		return nil, fmt.Errorf("libpag.esm.js not found in static directory (%s); to audit with official WASM, place libpag.wasm and libpag.esm.js in ./static", resolvedStatic)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed creating verification listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	var (
		resLock sync.Mutex
		result  *VerificationResult
		doneCh  = make(chan struct{})
		once    sync.Once
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/libpag.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, wasmPath)
	})

	mux.HandleFunc("/libpag.esm.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, esmPath)
	})

	mux.HandleFunc("/sample.pag", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(pagBytes)
	})

	mux.HandleFunc("/verify_result", func(w http.ResponseWriter, r *http.Request) {
		var vr VerificationResult
		if err := json.NewDecoder(r.Body).Decode(&vr); err != nil {
			vr = VerificationResult{
				Success: false,
				Error:   fmt.Sprintf("decode error: %v", err),
			}
		}

		resLock.Lock()
		result = &vr
		resLock.Unlock()

		once.Do(func() {
			close(doneCh)
		})
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="background: #000;">
    <canvas id="c" width="300" height="300"></canvas>
    <script type="module">
    import { PAGInit } from '/libpag.esm.js';

    async function run() {
        try {
            const PAG = await PAGInit({
                locateFile: (file) => '/' + file
            });
            const resp = await fetch('/sample.pag');
            const buf = await resp.arrayBuffer();
            const pagFile = await PAG.PAGFile.load(buf);
            if (!pagFile) {
                await fetch('/verify_result', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({ success: false, error: 'PAG.PAGFile.load returned null' })
                });
                return;
            }
            const res = {
                success: true,
                width: pagFile.width(),
                height: pagFile.height(),
                duration: pagFile.duration(),
                numTexts: pagFile.numTexts ? pagFile.numTexts() : 0,
                numImages: pagFile.numImages ? pagFile.numImages() : 0,
                numVideos: pagFile.numVideos ? pagFile.numVideos() : 0
            };
            await fetch('/verify_result', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(res)
            });
        } catch (e) {
            await fetch('/verify_result', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ success: false, error: e.toString() })
            });
        }
    }
    run();
    </script>
</body>
</html>`
		w.Write([]byte(html))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	cmd := exec.Command(
		edgePath,
		"--headless",
		"--disable-gpu",
		"--mute-audio",
		"--no-first-run",
		fmt.Sprintf("http://127.0.0.1:%d/index.html", port),
	)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed starting headless Edge at %s: %w", edgePath, err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	select {
	case <-doneCh:
		// Completed successfully
	case <-ctx.Done():
		return nil, errors.New("timeout verifying PAG file with libpag.wasm")
	}

	resLock.Lock()
	defer resLock.Unlock()

	if result == nil {
		return nil, errors.New("empty verification result received from browser")
	}
	if !result.Success {
		return result, fmt.Errorf("libpag verification failed: %s", result.Error)
	}

	return result, nil
}
