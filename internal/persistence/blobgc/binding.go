package blobgc

import (
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/payloadworker"
	application "retrom/internal/service/payloadrelease"
)

func Bind(executor dbapi.Executor) application.GCScope {
	return BindGC(executor, payloadworker.BindWorker(executor))
}
