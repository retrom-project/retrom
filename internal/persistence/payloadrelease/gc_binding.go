package payloadrelease

import (
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobgc"
	application "retrom/internal/service/payloadrelease"
)

func BindGC(executor dbapi.Executor) application.GCScope {
	return blobgc.BindGC(executor, BindWorker(executor))
}
