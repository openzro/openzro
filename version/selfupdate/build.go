package selfupdate

// buildTeamID is the Apple Developer Team ID openZro's installer is
// signed under. It is injected at release-build time via
//
//	-ldflags "-X github.com/openzro/openzro/version/selfupdate.buildTeamID=$APPLE_TEAM_ID"
//
// (the APPLE_TEAM_ID CI secret), exactly like version.version. It is
// deliberately NOT user/config-editable: an attacker who could rewrite
// it would defeat the S1 identity pin. A dev build leaves it empty,
// which makes Verify fail closed — dev builds must not self-update
// anyway (the gate also refuses the "development" version).
var buildTeamID string

// BuildTeamID returns the release-injected Apple Team ID (empty in dev
// builds). The daemon binding feeds this into selfupdate.Config so the
// engine stays pure/testable (tests pass an explicit ExpectedTeamID)
// while production pins to the build-time identity.
func BuildTeamID() string { return buildTeamID }
