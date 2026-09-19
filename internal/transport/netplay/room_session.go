package netplay

import (
	netplaymodel "retrom/internal/model/netplay"
	application "retrom/internal/service/netplay"
)

func validResyncSource(cause resyncCause, state string) bool {
	return application.ValidResyncSource(netplaymodel.ResyncCause(cause), state)
}
