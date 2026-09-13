package serverimport

import (
	"errors"

	"retrom/internal/cleanup"
)

func (service *Service) workerError(operation string, err error) {
	if errors.Is(err, ErrLeaseLost) || errors.Is(err, ErrWorkerCancelled) {
		return
	}
	cleanup.Error("server import "+operation, err)
}
