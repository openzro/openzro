package server

import (
	"os"
	"strconv"
)

// LocalUsersEnabledEnv opts in to SCIM-provisioned local users. Off by
// default: enable it only once every management replica runs a version
// with the local-user store guards, because an older replica's
// SaveAccount does not take the account mutex (#205).
const LocalUsersEnabledEnv = "OPENZRO_SCIM_LOCAL_USERS_ENABLED"

// LocalUsersEnabledFromEnv reports whether the operator opted in. Anything
// that does not parse as true leaves the feature off.
func LocalUsersEnabledFromEnv() bool {
	enabled, err := strconv.ParseBool(os.Getenv(LocalUsersEnabledEnv))
	return err == nil && enabled
}
