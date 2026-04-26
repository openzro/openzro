//go:build !android

package iface

import (
	"golang.zx2c4.com/wireguard/tun/netstack"

	wgdevice "golang.zx2c4.com/wireguard/device"

	"github.com/openzro/openzro/client/iface/bind"
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/iface/wgaddr"
)

type WGTunDevice interface {
	Create() (device.WGConfigurer, error)
	Up() (*bind.UniversalUDPMuxDefault, error)
	UpdateAddr(address wgaddr.Address) error
	WgAddress() wgaddr.Address
	DeviceName() string
	Close() error
	FilteredDevice() *device.FilteredDevice
	Device() *wgdevice.Device
	GetNet() *netstack.Net
}
