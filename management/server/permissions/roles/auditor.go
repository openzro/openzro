package roles

import (
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/types"
)

var Auditor = RolePermissions{
	Role: types.UserRoleAuditor,
	AutoAllowNew: map[operations.Operation]bool{
		operations.Read:   true,
		operations.Create: false,
		operations.Update: false,
		operations.Delete: false,
	},
}
