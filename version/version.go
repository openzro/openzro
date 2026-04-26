package version

import (
	"regexp"

	v "github.com/hashicorp/go-version"
)

// will be replaced with the release version when using goreleaser
var version = "development"

var (
	VersionRegexp = regexp.MustCompile("^" + v.VersionRegexpRaw + "$")
	SemverRegexp  = regexp.MustCompile("^" + v.SemverRegexpRaw + "$")
)

// OpenzroVersion returns the Openzro version
func OpenzroVersion() string {
	return version
}
