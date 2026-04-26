package peer

// Listener is a callback type about the Openzro network connection state
type Listener interface {
	OnConnected()
	OnDisconnected()
	OnConnecting()
	OnDisconnecting()
	OnAddressChanged(string, string)
	OnPeersListChanged(int)
}
