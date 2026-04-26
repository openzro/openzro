//go:build !android

package dns

import (
	"github.com/openzro/openzro/client/configs"
	"os"
	"path/filepath"
)

var fileUncleanShutdownResolvConfLocation string

func init() {
	fileUncleanShutdownResolvConfLocation = os.Getenv("OZ_UNCLEAN_SHUTDOWN_RESOLV_FILE")
	if fileUncleanShutdownResolvConfLocation == "" {
		fileUncleanShutdownResolvConfLocation = filepath.Join(configs.StateDir, "resolv.conf")
	}
}
