package netplay

import (
	netplaymodel "retrom/internal/model/netplay"
	netplayservice "retrom/internal/service/netplay"
)

func validResyncSource(cause resyncCause, state string) bool {
	return netplayservice.ValidResyncSource(netplaymodel.ResyncCause(cause), state)
}
