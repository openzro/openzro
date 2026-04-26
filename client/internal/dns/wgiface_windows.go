package dns

import (
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/iface/wgaddr"
)

// WGIface defines subset methods of interface required for manager
type WGIface interface {
	Name() string
	Address() wgaddr.Address
	IsUserspaceBind() bool
	GetFilter() device.PacketFilter
	GetDevice() *device.FilteredDevice
	GetInterfaceGUIDString() (string, error)
}
