//go:build !(linux && 386) && !windows

package main

import (
	_ "embed"
)

//go:embed assets/openzro.png
var iconAbout []byte

//go:embed assets/openzro-systemtray-connected.png
var iconConnected []byte

//go:embed assets/openzro-systemtray-connected-dark.png
var iconConnectedDark []byte

//go:embed assets/openzro-systemtray-disconnected.png
var iconDisconnected []byte

//go:embed assets/openzro-systemtray-update-disconnected.png
var iconUpdateDisconnected []byte

//go:embed assets/openzro-systemtray-update-disconnected-dark.png
var iconUpdateDisconnectedDark []byte

//go:embed assets/openzro-systemtray-update-connected.png
var iconUpdateConnected []byte

//go:embed assets/openzro-systemtray-update-connected-dark.png
var iconUpdateConnectedDark []byte

//go:embed assets/openzro-systemtray-connecting.png
var iconConnecting []byte

//go:embed assets/openzro-systemtray-connecting-dark.png
var iconConnectingDark []byte

//go:embed assets/openzro-systemtray-error.png
var iconError []byte

//go:embed assets/openzro-systemtray-error-dark.png
var iconErrorDark []byte
