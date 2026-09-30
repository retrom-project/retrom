package libraryimport

import (
	"context"
	"testing"
	"time"

	"retrom/internal/service/jobs"
)

func TestMultiDiscAttachmentInputSupportsGenericRetry(t *testing.T) {
	write := MultiDiscAttachmentWrite{Input: MultiDiscAttachmentInput{
		SchemaVersion: 1, AttachmentID: "attachment", ImportItemID: "item",
	}, JobID: "job"}
	if err := encodeMultiDiscAdmission(&write); err != nil {
		t.Fatal(err)
	}
	repository := &attachmentRetryRecords{input: []byte(write.InputJSON)}
	result, err := jobs.New(repository, func() time.Time { return time.UnixMilli(20) }).Retry(t.Context(), "job", 1)
	if err != nil || result.ExecutionNo != 2 || repository.change == nil {
		t.Fatalf("retry frozen attachment input: result=%+v change=%+v err=%v", result, repository.change, err)
	}
}

type attachmentRetryRecords struct {
	input  []byte
	change *jobs.RetryWrite
}

func (records *attachmentRetryRecords) WithWrite(_ context.Context, run func(jobs.Records) error) error {
	return run(records)
}

func (*attachmentRetryRecords) WithRead(context.Context, func(jobs.ReadRecords) error) error {
	panic("unexpected read")
}

func (*attachmentRetryRecords) Get(context.Context, string) (jobs.Job, error) {
	return jobs.Job{
		Kind: "REVIEW_MULTI_DISC_VALIDATE", ScopeType: "IMPORT_ITEM", ScopeID: "item",
		State: "FAILED", Retryable: true, ExecutionNo: 1, Version: 1,
	}, nil
}

func (records *attachmentRetryRecords) Input(context.Context, string, int64) ([]byte, error) {
	return records.input, nil
}

func (records *attachmentRetryRecords) Retry(_ context.Context, change jobs.RetryWrite) error {
	records.change = &change
	return nil
}

func (*attachmentRetryRecords) Cancel(context.Context, jobs.Cancellation) error {
	panic("unexpected cancel")
}

func (*attachmentRetryRecords) CancelServerImport(context.Context, jobs.Cancellation) error {
	panic("unexpected server import")
}
