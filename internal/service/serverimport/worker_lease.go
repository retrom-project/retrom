package serverimport

import (
	"errors"
	model "retrom/internal/model/serverimport"

	"retrom/internal/foundation/cleanup"
)

func (service *Service) workerError(operation string, err error) {
	if errors.Is(err, model.ErrLeaseLost) || errors.Is(err, model.ErrWorkerCancelled) {
		return
	}
	cleanup.Error("server import "+operation, err)
}
