package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
)

// browserOpenArgs maps an OS to the command that opens a file with the default
// handler. Windows uses rundll32 so a plain path with no protocol still opens.
func browserOpenArgs(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		return "xdg-open", []string{path}
	}
}

// openInBrowser opens path with the OS default handler. Opening is best-effort:
// on a headless or CI machine with no opener, it logs a warning and returns nil
// so the run still succeeds with the file already written.
func openInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	bin, args := browserOpenArgs(runtime.GOOS, abs)
	resolved, err := exec.LookPath(bin)
	if err != nil {
		log.Printf("warning: could not open %s automatically: %s not found", abs, bin)
		return nil
	}
	if err := exec.Command(resolved, args...).Start(); err != nil {
		log.Printf("warning: could not open %s automatically: %s", abs, err)
		return nil
	}
	return nil
}
