package metadatascrape

import (
	"context"
	"errors"
	model "retrom/internal/model/metadatascrape"
	"testing"
	"time"
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

type subjectMemory struct {
	model.ScheduleReader
	game  model.GameSubject
	found bool
	err   error
}

func (reader subjectMemory) Game(context.Context, string) (model.GameSubject, bool, error) {
	return reader.game, reader.found, reader.err
}

type evidenceMemory struct {
	model.ScheduleEvidenceReader
	files []model.FileEvidence
}

func (reader evidenceMemory) Files(context.Context, model.Subject) ([]model.FileEvidence, error) {
	return reader.files, nil
}

type schedulingMemory struct {
	scope     model.ScheduleScope
	lateError error
	committed bool
}

func (repository *schedulingMemory) CommitWrite(ctx context.Context, work func(model.ScheduleScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := work(repository.scope); err != nil {
		return err
	}
	if repository.lateError != nil {
		return repository.lateError
	}
	repository.committed = true
	return nil
}

func TestScrapeGameChecksSubjectBeforeCreatingTask(t *testing.T) {
	for _, test := range []struct {
		name   string
		reader subjectMemory
		want   error
	}{
		{"missing", subjectMemory{}, model.ErrGameVersionConflict},
		{"stale", subjectMemory{found: true, game: model.GameSubject{Version: 2}}, model.ErrGameVersionConflict},
		{"storage failure", subjectMemory{err: context.Canceled}, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			writes := &scheduleMemory{}
			repository := &schedulingMemory{scope: model.ScheduleScope{Subjects: test.reader, Writes: writes}}
			_, _, err := NewScheduler(repository, nil, time.Now).ScheduleGame(t.Context(), "game", 1)
			if !errors.Is(err, test.want) || writes.creates != 0 || repository.committed {
				t.Fatalf("invalid scheduling: writes=%d committed=%t error=%v", writes.creates, repository.committed, err)
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
	writer := &scheduleMemory{}
	repository := &schedulingMemory{scope: model.ScheduleScope{
		Subjects: subjectMemory{found: true, game: model.GameSubject{Version: 1, PlatformID: "gba", ManifestDigest: "manifest"}},
		Sources:  evidenceMemory{}, Writes: writer,
	}, lateError: context.DeadlineExceeded}
	_, _, err := NewScheduler(repository, nil, func() time.Time { return time.UnixMilli(100) }).ScheduleGame(t.Context(), "game", 1)
	if !errors.Is(err, context.DeadlineExceeded) || repository.committed || writer.creates != 1 {
		t.Fatalf("late scheduling failure: %v", err)
	}
}
