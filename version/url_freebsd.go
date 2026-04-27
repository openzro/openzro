package version

// DownloadUrl returns the URL the UI shows when prompting the user
// to download a newer release. FreeBSD points at the GitHub Releases
// page like every other platform.
func DownloadUrl() string {
	return resolvedDownloadURL()
}
