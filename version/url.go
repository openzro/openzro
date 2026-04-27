package version

import "os"

// defaultDownloadURL points at the GitHub Releases page for the
// project. openZro is GitHub-Releases-only; there is no
// `pkgs.openzro.io` (the upstream's package CDN is not mirrored).
//
// Operators who run their own internal mirror can override via the
// OPENZRO_DOWNLOAD_URL environment variable — useful for air-gapped
// deployments or for running a corporate mirror behind an HMAC-
// signed CDN.
const defaultDownloadURL = "https://github.com/openzro/openzro/releases/latest"

const envDownloadURL = "OPENZRO_DOWNLOAD_URL"

func resolvedDownloadURL() string {
	if v, ok := os.LookupEnv(envDownloadURL); ok && v != "" {
		return v
	}
	return defaultDownloadURL
}
