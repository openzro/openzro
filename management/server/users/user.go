package users

import (
	"github.com/openzro/openzro/management/server/permissions/roles"
	"github.com/openzro/openzro/management/server/types"
)

// Wrapped UserInfo with Role Permissions
type UserInfoWithPermissions struct {
	*types.UserInfo

	Permissions roles.Permissions
	Restricted  bool
}
