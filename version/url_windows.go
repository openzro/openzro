package version

// DownloadUrl returns the URL the UI shows when prompting the user
// to download a newer release. Windows: the upstream probed the
// HKLM "App Paths\Openzro" registry key to decide between the
// installer URL and the generic page. openZro does not ship a
// signed Windows installer (yet), so the registry probe was
// always returning the fallback path. Simplified to point
// straight at GitHub Releases for every Windows agent.
func DownloadUrl() string {
	return resolvedDownloadURL()
}
