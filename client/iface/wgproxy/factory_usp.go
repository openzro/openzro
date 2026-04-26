package wgproxy

import (
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/iface/bind"
	proxyBind "github.com/openzro/openzro/client/iface/wgproxy/bind"
)

type USPFactory struct {
	bind *bind.ICEBind
}

func NewUSPFactory(iceBind *bind.ICEBind) *USPFactory {
	log.Infof("WireGuard Proxy Factory will produce bind proxy")
	f := &USPFactory{
		bind: iceBind,
	}
	return f
}

func (w *USPFactory) GetProxy() Proxy {
	return proxyBind.NewProxyBind(w.bind)
}

func (w *USPFactory) Free() error {
	return nil
}
