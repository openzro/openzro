//go:build !android

package ice

import (
	"github.com/openzro/openzro/client/internal/stdnet"
)

func newStdNet(_ stdnet.ExternalIFaceDiscover, ifaceBlacklist []string) (*stdnet.Net, error) {
	return stdnet.NewNet(ifaceBlacklist)
}
