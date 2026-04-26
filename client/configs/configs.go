package configs

import (
	"os"
	"path/filepath"
	"runtime"
)

var StateDir string

func init() {
	StateDir = os.Getenv("OZ_STATE_DIR")
	if StateDir != "" {
		return
	}
	switch runtime.GOOS {
	case "windows":
		StateDir = filepath.Join(os.Getenv("PROGRAMDATA"), "Openzro")
	case "darwin", "linux":
		StateDir = "/var/lib/openzro"
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		StateDir = "/var/db/openzro"
	}
}
