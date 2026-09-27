package filedeletion

import (
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/cleanupjobs"
	application "retrom/internal/service/cleanupjobs"
)

func Bind(executor dbapi.Executor) application.DeletionScope {
	return BindQueue(executor, cleanupjobs.BindWorker(executor))
}
