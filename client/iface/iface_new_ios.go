//go:build ios

package iface

import (
	"github.com/openzro/openzro/client/iface/bind"
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/iface/wgaddr"
	"github.com/openzro/openzro/client/iface/wgproxy"
)

// NewWGIFace Creates a new WireGuard interface instance
func NewWGIFace(opts WGIFaceOpts) (*WGIface, error) {
	wgAddress, err := wgaddr.ParseWGAddress(opts.Address)
	if err != nil {
		return nil, err
	}

	iceBind := bind.NewICEBind(opts.TransportNet, opts.FilterFn, wgAddress)

	wgIFace := &WGIface{
		tun:            device.NewTunDevice(opts.IFaceName, wgAddress, opts.WGPort, opts.WGPrivKey, iceBind, opts.MobileArgs.TunFd),
		userspaceBind:  true,
		wgProxyFactory: wgproxy.NewUSPFactory(iceBind),
	}
	return wgIFace, nil
}
