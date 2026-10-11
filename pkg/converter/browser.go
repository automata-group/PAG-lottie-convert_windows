package converter

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// FindBrowserExecutable discovers an available Chromium-based browser (Edge, Chrome, Chromium)
// dynamically across Windows, Linux, and macOS without hardcoding paths.
func FindBrowserExecutable(customPath string) (string, error) {
	if customPath != "" {
		if _, err := exec.LookPath(customPath); err == nil {
			return customPath, nil
		}
		if fi, err := os.Stat(customPath); err == nil && !fi.IsDir() {
			return customPath, nil
		}
	}

	// 1. Search PATH for standard browser binaries
	for _, binName := range []string{"msedge", "chrome", "google-chrome", "chromium", "chromium-browser", "brave"} {
		if path, err := exec.LookPath(binName); err == nil {
			return path, nil
		}
	}

	// 2. Windows standard installation locations
	localAppData := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")

	candidates := []string{
		// Microsoft Edge (x64 / x86 / user)
		filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),
		// Google Chrome (x64 / x86 / user)
		filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
		// Brave
		filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}

	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, nil
		}
	}

	// 3. Unix / Linux standard paths
	unixPaths := []string{
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/bin/microsoft-edge",
		"/snap/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	}
	for _, p := range unixPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}

	return "", errors.New("no Chromium-based browser (Edge, Chrome, Chromium) detected; please install Edge or Chrome, or specify via -edge flag")
}

// ResolveStaticDir locates an optional directory containing static assets (e.g. libpag.wasm)
// without using any machine-specific hardcoded paths.
func ResolveStaticDir(customDir string) string {
	candidates := []string{
		customDir,
		"./static",
		"../static",
		"../../static",
		"../../../static",
		filepath.Join(filepath.Dir(os.Args[0]), "static"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "static"),
	}

	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		wasmPath := filepath.Join(dir, "libpag.wasm")
		if info, err := os.Stat(wasmPath); err == nil && !info.IsDir() {
			return dir
		}
	}
	return customDir
}

// KillProcessTree forcefully terminates a process and all its child/descendant processes.
// On Windows, it invokes 'taskkill /T /F /PID <pid>' to eliminate all child Chromium renderer, GPU,
// and crashpad processes, preventing any orphaned zombie processes from lingering in Task Manager.
// On other operating systems, it terminates the process directly.
func KillProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
		_ = killCmd.Run()
	} else {
		_ = cmd.Process.Kill()
	}
}
