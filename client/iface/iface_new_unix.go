//go:build (linux && !android) || freebsd

package iface

import (
	"fmt"

	"github.com/openzro/openzro/client/iface/bind"
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/iface/netstack"
	"github.com/openzro/openzro/client/iface/wgaddr"
	"github.com/openzro/openzro/client/iface/wgproxy"
)

// NewWGIFace Creates a new WireGuard interface instance
func NewWGIFace(opts WGIFaceOpts) (*WGIface, error) {
	wgAddress, err := wgaddr.ParseWGAddress(opts.Address)
	if err != nil {
		return nil, err
	}

	wgIFace := &WGIface{}

	if netstack.IsEnabled() {
		iceBind := bind.NewICEBind(opts.TransportNet, opts.FilterFn, wgAddress)
		wgIFace.tun = device.NewNetstackDevice(opts.IFaceName, wgAddress, opts.WGPort, opts.WGPrivKey, opts.MTU, iceBind, netstack.ListenAddr())
		wgIFace.userspaceBind = true
		wgIFace.wgProxyFactory = wgproxy.NewUSPFactory(iceBind)
		return wgIFace, nil
	}

	if device.WireGuardModuleIsLoaded() {
		wgIFace.tun = device.NewKernelDevice(opts.IFaceName, wgAddress, opts.WGPort, opts.WGPrivKey, opts.MTU, opts.TransportNet)
		wgIFace.wgProxyFactory = wgproxy.NewKernelFactory(opts.WGPort)
		return wgIFace, nil
	}
	if device.ModuleTunIsLoaded() {
		iceBind := bind.NewICEBind(opts.TransportNet, opts.FilterFn, wgAddress)
		wgIFace.tun = device.NewUSPDevice(opts.IFaceName, wgAddress, opts.WGPort, opts.WGPrivKey, opts.MTU, iceBind)
		wgIFace.userspaceBind = true
		wgIFace.wgProxyFactory = wgproxy.NewUSPFactory(iceBind)
		return wgIFace, nil
	}

	return nil, fmt.Errorf("couldn't check or load tun module")
}
