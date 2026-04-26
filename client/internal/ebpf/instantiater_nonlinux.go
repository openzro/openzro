//go:build !linux || android

package ebpf

import "github.com/openzro/openzro/client/internal/ebpf/manager"

// GetEbpfManagerInstance return error because ebpf is not supported on all os
func GetEbpfManagerInstance() manager.Manager {
	panic("unsupported os")
}
