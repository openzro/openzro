package desktop

import "github.com/openzro/openzro/version"

// GetUIUserAgent returns the Desktop ui user agent
func GetUIUserAgent() string {
	return "openzro-desktop-ui/" + version.OpenzroVersion()
}
