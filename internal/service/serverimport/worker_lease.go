package serverimport

import (
	"errors"

	"retrom/internal/foundation/cleanup"
	model "retrom/internal/model/serverimport"
)

func (service *Service) workerError(operation string, err error) {
	if errors.Is(err, model.ErrLeaseLost) || errors.Is(err, model.ErrWorkerCancelled) {
		return
	}
	cleanup.Error("server import "+operation, err)
}
