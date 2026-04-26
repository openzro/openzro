package ice

import "github.com/openzro/openzro/client/internal/stdnet"

func newStdNet(iFaceDiscover stdnet.ExternalIFaceDiscover, ifaceBlacklist []string) (*stdnet.Net, error) {
	return stdnet.NewNetWithDiscover(iFaceDiscover, ifaceBlacklist)
}
