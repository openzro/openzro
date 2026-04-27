package version

// DownloadUrl returns the URL the UI shows when prompting the user
// to download a newer release. macOS: the upstream branched on a
// brew-formula probe vs a per-arch package URL; openZro does not
// ship a brew tap nor per-arch package URLs (yet), so all paths
// land on the GitHub Releases page.
func DownloadUrl() string {
	return resolvedDownloadURL()
}
