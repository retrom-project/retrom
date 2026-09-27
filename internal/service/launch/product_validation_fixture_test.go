package launch

import (
	"context"

	gamevariant "retrom/internal/service/gamevariant"
)

type productValidationMemory struct {
	current      gamevariant.ValidationJob
	found        bool
	failure      error
	writeFailure error
	writes       []gamevariant.ValidationJobWrite
}

func (repository *productValidationMemory) Find(context.Context, string) (gamevariant.ValidationJob, bool, error) {
	return repository.current, repository.found, repository.failure
}

func (repository *productValidationMemory) Write(_ context.Context, plan gamevariant.ValidationJobWrite) error {
	if repository.writeFailure != nil {
		return repository.writeFailure
	}
	repository.writes = append(repository.writes, plan)
	return nil
}
