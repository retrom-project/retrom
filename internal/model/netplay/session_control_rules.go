package netplay

// ControlPeer finds the peer matching the given identity in the session snapshot.
func ControlPeer(before SessionControlSnapshot, identity PeerIdentity) (SessionPeer, error) {
	for _, peer := range before.Peers {
		if peer.ProfileID == identity.ProfileID && peer.PlayerNo == identity.PlayerNo &&
			peer.CredentialGeneration == identity.CredentialGeneration {
			return peer, nil
		}
	}
	return SessionPeer{}, ErrForbidden
}

// AllControlPeersReady checks whether all peers in the session are RUNTIME_READY.
func AllControlPeersReady(before SessionControlSnapshot, changed SessionPeer) bool {
	if len(before.Peers) < 2 {
		return false
	}
	for _, peer := range before.Peers {
		if peer.ProfileID == changed.ProfileID {
			peer = changed
		}
		if peer.State != "RUNTIME_READY" {
			return false
		}
	}
	return true
}

// ValidResyncSource checks whether the given resync cause is valid for the
// current session state.
func ValidResyncSource(cause ResyncCause, state string) bool {
	switch cause {
	case ResyncReconnect:
		return state == "PAUSED_RECONNECT" || state == "RUNNING"
	case ResyncHash:
		return state == "RUNNING"
	case ResyncHost:
		return state == "PAUSED_RECONNECT"
	default:
		return false
	}
}
