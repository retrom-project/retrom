package serverimport

import (
	"errors"

	"retrom/internal/cleanup"
	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func (service *Service) leases() *importservice.Leases {
	return importservice.NewLeases(importpersistence.NewLeases(service.database), service.now)
}

func (service *Service) workerError(operation string, err error) {
	if errors.Is(err, importservice.ErrLeaseLost) || errors.Is(err, importservice.ErrWorkerCancelled) {
		return
	}
	cleanup.Error("server import "+operation, err)
}
