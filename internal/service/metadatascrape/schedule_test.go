package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/metadatascrape"
)

type scheduleMemory struct {
	model.ScheduleWriter
	plan     model.SchedulePlan
	creates  int
	evidence []model.HashEvidence
}

func (writer *scheduleMemory) Create(_ context.Context, plan model.SchedulePlan) error {
	writer.plan = plan
	writer.creates++
	return nil
}

func (writer *scheduleMemory) Evidence(_ context.Context, values []model.HashEvidence) error {
	writer.evidence = values
	return nil
}
func (writer *scheduleMemory) Game(context.Context, string, int64, int64) error { return nil }

type evidenceMemory struct {
	model.ScheduleEvidenceReader
	files []model.FileEvidence
}

func (reader evidenceMemory) Files(context.Context, model.Subject) ([]model.FileEvidence, error) {
	return reader.files, nil
}

type schedulingMemory struct {
	reviewResult model.ScheduleResult
	gameResult   model.ScheduleResult
	reviewCmd    model.ReviewScheduleCommand
	gameCmd      model.GameScheduleCommand
	reviewErr    error
	gameErr      error
}

func (repository *schedulingMemory) CommitReviewSchedule(
	_ context.Context, cmd model.ReviewScheduleCommand,
) (model.ScheduleResult, error) {
	repository.reviewCmd = cmd
	return repository.reviewResult, repository.reviewErr
}

func (repository *schedulingMemory) CommitGameSchedule(
	_ context.Context, cmd model.GameScheduleCommand,
) (model.ScheduleResult, error) {
	repository.gameCmd = cmd
	return repository.gameResult, repository.gameErr
}

func TestScrapeGameScheduleFailureIsReturned(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{"version conflict", model.ErrGameVersionConflict},
		{"storage failure", context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &schedulingMemory{gameErr: test.err}
			_, _, err := NewScheduler(repository, nil, time.Now).ScheduleGame(t.Context(), "game", 1)
			if !errors.Is(err, test.err) {
				t.Fatalf("schedule game error: %v", err)
			}
		})
	}
}

func TestDisabledProviderCreatesCompletedTaskWithoutReadingEvidence(t *testing.T) {
	writer := &scheduleMemory{}
	scheduled, err := NewScheduler(nil, nil, func() time.Time { return time.UnixMilli(100) }).
		ScheduleImport(t.Context(), model.ScheduleScope{Writes: writer}, "item", "NONE")
	if err != nil {
		t.Fatal(err)
	}
	plan := writer.plan
	if !scheduled.Noop || plan.JobState != "SUCCEEDED" || plan.RunState != "COMPLETED" || plan.FinishedAt == nil || *plan.FinishedAt != 100 {
		t.Fatalf("disabled scrape task: %+v / %+v", scheduled, plan)
	}
}

func TestRawAndArchiveEvidenceUseDifferentHashIdentities(t *testing.T) {
	archive := "archive"
	ordinal := int64(3)
	source := evidenceMemory{files: []model.FileEvidence{
		{Name: "unexpanded.ZIP", BlobID: "skip"},
		{Name: "game.gba", BlobID: "raw"},
		{Name: "member.gba", BlobID: "expanded", ArchiveBlobID: &archive, ArchiveOrdinal: &ordinal},
	}}
	result, err := contentEvidence(t.Context(), source, model.SchedulePlan{RunID: "run", Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].Profile != "RAW_FILE" || *result[0].BlobID != "raw" || result[0].Order != 0 {
		t.Fatalf("raw evidence: %+v", result)
	}
	member := result[1]
	if member.Profile != "SINGLE_ARCHIVE_MEMBER" || member.BlobID != nil || *member.ArchiveBlobID != archive || *member.ArchiveOrdinal != ordinal || member.Order != 1 {
		t.Fatalf("archive evidence: %+v", member)
	}
}

func TestArcadeEvidencePrefersSHA1DeduplicatesAndCapsQueries(t *testing.T) {
	entries := make([]model.ArcadeEvidence, 0, 11)
	crc := "crc"
	entries = append(entries, model.ArcadeEvidence{Name: "crc-only", Size: 999, CRC32: &crc})
	for _, name := range []string{"i", "h", "g", "f", "e", "d", "c", "b", "a"} {
		entries = append(entries, model.ArcadeEvidence{Name: name, SHA1: &name, Size: 10, ArchiveBlobID: name})
	}
	entries = append(entries, entries[9])
	evidence, err := selectArcadeEvidence(entries, model.SchedulePlan{RunID: "run", Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 8 {
		t.Fatalf("query count=%d", len(evidence))
	}
	for index, item := range evidence {
		want := string(rune('a' + index))
		if item.Profile != "ARCADE_DAT_ENTRIES" || item.Order != index || item.SHA1 == nil || *item.SHA1 != want || item.BlobID != nil {
			t.Fatalf("arcade query %d: %+v", index, item)
		}
	}
}

func TestScheduleCommitFailureIsReturned(t *testing.T) {
	repository := &schedulingMemory{gameErr: context.DeadlineExceeded}
	_, _, err := NewScheduler(repository, nil, func() time.Time { return time.UnixMilli(100) }).ScheduleGame(t.Context(), "game", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late scheduling failure: %v", err)
	}
}
