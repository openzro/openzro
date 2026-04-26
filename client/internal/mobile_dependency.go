package internal

import (
	"github.com/openzro/openzro/client/iface/device"
	"github.com/openzro/openzro/client/internal/dns"
	"github.com/openzro/openzro/client/internal/listener"
	"github.com/openzro/openzro/client/internal/stdnet"
)

// MobileDependency collect all dependencies for mobile platform
type MobileDependency struct {
	// Android only
	TunAdapter            device.TunAdapter
	IFaceDiscover         stdnet.ExternalIFaceDiscover
	NetworkChangeListener listener.NetworkChangeListener
	HostDNSAddresses      []string
	DnsReadyListener      dns.ReadyListener

	//	iOS only
	DnsManager     dns.IosDnsManager
	FileDescriptor int32
	StateFilePath  string
}
