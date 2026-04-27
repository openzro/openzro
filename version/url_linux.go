package version

// DownloadUrl returns the URL the UI shows when prompting the user
// to download a newer release. Linux does not have a per-arch
// installer URL because openZro publishes everything as a single
// GitHub Release page (all assets live there).
func DownloadUrl() string {
	return resolvedDownloadURL()
}
