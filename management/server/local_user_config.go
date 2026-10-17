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

// LocalUsersDexStorageWarning is logged whenever local users are enabled.
// Management cannot see which storage Dex uses, and on SQLite or memory
// storage Dex v2.45-v2.46 deadlocks its whole token endpoint on the first
// refresh of a password-DB user (dexidp/dex#4209; fix pending in
// dexidp/dex#4991). The Helm chart points Dex at Postgres when the store is
// Postgres; the docker-compose quickstart uses SQLite.
const LocalUsersDexStorageWarning = "SCIM local users require Dex to use Postgres storage: with SQLite or " +
	"memory storage, Dex v2.45-v2.46 deadlocks its token endpoint on the first session refresh of a " +
	"local user and every later login hangs until Dex restarts (dexidp/dex#4209, fix pending in dexidp/dex#4991)"
