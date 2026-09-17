package netplay

import (
	netplaymodel "retrom/internal/model/netplay"
)

func validResyncSource(cause resyncCause, state string) bool {
	return netplaymodel.ValidResyncSource(netplaymodel.ResyncCause(cause), state)
}
