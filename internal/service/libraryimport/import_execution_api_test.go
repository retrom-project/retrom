package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"
)

type importWorkerRepositoryProbe struct{ writes int }

func (probe *importWorkerRepositoryProbe) WithExecution(context.Context, func(model.ImportExecutionScope) error) error {
	probe.writes++
	return errors.New("unexpected worker write")
}

func (*importWorkerRepositoryProbe) Queued(context.Context, int64) ([]string, error) {
	return nil, errors.New("unexpected queue read")
}

func (*importWorkerRepositoryProbe) Recoverable(context.Context, int64) ([]string, error) {
	return nil, errors.New("unexpected recovery read")
}

func TestImportWorkerIdentityFailurePrecedesWrite(t *testing.T) {
	repository := &importWorkerRepositoryProbe{}
	service := NewImportExecutions(repository, nil)
	cause := errors.New("worker entropy unavailable")
	service.newID = func() (string, error) { return "", cause }
	work, found, err := service.Claim(t.Context(), "job")
	if !errors.Is(err, cause) || found || work.Execution.JobID != "" || repository.writes != 0 {
		t.Fatalf("worker identity failure: work=%+v found=%t writes=%d error=%v", work, found, repository.writes, err)
	}
}
