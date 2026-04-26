package main

import (
	_ "embed"
)

//go:embed assets/openzro.ico
var iconAbout []byte

//go:embed assets/openzro-systemtray-connected.ico
var iconConnected []byte

//go:embed assets/openzro-systemtray-connected-dark.ico
var iconConnectedDark []byte

//go:embed assets/openzro-systemtray-disconnected.ico
var iconDisconnected []byte

//go:embed assets/openzro-systemtray-update-disconnected.ico
var iconUpdateDisconnected []byte

//go:embed assets/openzro-systemtray-update-disconnected-dark.ico
var iconUpdateDisconnectedDark []byte

//go:embed assets/openzro-systemtray-update-connected.ico
var iconUpdateConnected []byte

//go:embed assets/openzro-systemtray-update-connected-dark.ico
var iconUpdateConnectedDark []byte

//go:embed assets/openzro-systemtray-connecting.ico
var iconConnecting []byte

//go:embed assets/openzro-systemtray-connecting-dark.ico
var iconConnectingDark []byte

//go:embed assets/openzro-systemtray-error.ico
var iconError []byte

//go:embed assets/openzro-systemtray-error-dark.ico
var iconErrorDark []byte
