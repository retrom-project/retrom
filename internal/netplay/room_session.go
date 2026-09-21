package netplay

import application "retrom/internal/service/netplay"

func validResyncSource(cause resyncCause, state string) bool {
	return application.ValidResyncSource(application.ResyncCause(cause), state)
}
