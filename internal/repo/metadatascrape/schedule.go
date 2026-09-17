package metadatascrape

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/capability/security/authn"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"

	"github.com/google/uuid"
)

type (
	ScheduleRepository struct {
		database      *sql.DB
		preCommitHook func() error
	}
	scheduleReads  struct{ database dbexec.Executor }
	scheduleWrites struct{ transaction *sql.Tx }
)

func NewScheduler(database *sql.DB) *ScheduleRepository {
	return &ScheduleRepository{database: database}
}

func WithSchedulePreCommitHook(repo *ScheduleRepository, hook func() error) {
	repo.preCommitHook = hook
}

func BindSchedule(transaction *sql.Tx) metadatascrape.ScheduleScope {
	reader := scheduleReads{transaction}
	return metadatascrape.ScheduleScope{Subjects: reader, Sources: reader, Writes: scheduleWrites{transaction}}
}

func (repository *ScheduleRepository) CommitReviewSchedule(
	ctx context.Context, cmd metadatascrape.ReviewScheduleCommand,
) (metadatascrape.ScheduleResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("begin review scrape scheduling: %w", err)
	}
	defer dbexec.Rollback(tx)
	reads := scheduleReads{tx}
	writes := scheduleWrites{tx}

	draft, found, err := reads.Review(ctx, cmd.ItemID)
	if err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("read review scrape subject: %w", err)
	}
	if !found || draft.Version != cmd.Version {
		return metadatascrape.ScheduleResult{}, metadatascrape.ErrReviewVersionConflict
	}

	nonce, err := scheduleUUID()
	if err != nil {
		return metadatascrape.ScheduleResult{}, err
	}
	plan, err := buildSchedulePlan(
		metadatascrape.Subject{Kind: "IMPORT_ITEM", ID: cmd.ItemID},
		cmd.Provider,
		"metadata-review-v1:"+cmd.ItemID+":"+nonce,
		map[string]any{"provider": cmd.Provider, "bypassCache": true},
		cmd.Now,
	)
	if err != nil {
		return metadatascrape.ScheduleResult{}, err
	}
	if err := writes.Create(ctx, plan); err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("create import scrape: %w", err)
	}
	if cmd.Provider != "NONE" {
		item, err := reads.Import(ctx, cmd.ItemID)
		if err != nil {
			return metadatascrape.ScheduleResult{}, fmt.Errorf("read scrape import subject: %w", err)
		}
		if err := buildAndWriteEvidence(ctx, reads, writes, plan, item.PlatformID); err != nil {
			return metadatascrape.ScheduleResult{}, err
		}
	}
	if err := writeReviewRequest(
		ctx, writes, cmd.ItemID, cmd.Provider, draft,
		metadatascrape.ScheduleResult{RunID: plan.RunID, JobID: plan.JobID}, cmd.Actor, cmd.Now,
	); err != nil {
		return metadatascrape.ScheduleResult{}, err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return metadatascrape.ScheduleResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("commit review scrape scheduling: %w", err)
	}
	noop := cmd.Provider == "NONE"
	return metadatascrape.ScheduleResult{RunID: plan.RunID, JobID: plan.JobID, Noop: noop}, nil
}

func (repository *ScheduleRepository) CommitGameSchedule(
	ctx context.Context, cmd metadatascrape.GameScheduleCommand,
) (metadatascrape.ScheduleResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("begin game scrape scheduling: %w", err)
	}
	defer dbexec.Rollback(tx)
	reads := scheduleReads{tx}
	writes := scheduleWrites{tx}

	game, found, err := reads.Game(ctx, cmd.GameID)
	if err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("read game scrape subject: %w", err)
	}
	if !found || game.Version != cmd.Version {
		return metadatascrape.ScheduleResult{}, metadatascrape.ErrGameVersionConflict
	}

	plan, err := buildSchedulePlan(
		metadatascrape.Subject{Kind: "GAME", ID: cmd.GameID},
		"HASHEOUS",
		"metadata-game-v1:"+cmd.GameID+":"+game.ManifestDigest,
		map[string]any{
			"gameId":               cmd.GameID,
			"sourceManifestDigest": game.ManifestDigest,
			"provider":             "HASHEOUS",
			"bypassCache":          true,
		},
		cmd.Now,
	)
	if err != nil {
		return metadatascrape.ScheduleResult{}, err
	}
	if err := writes.Create(ctx, plan); err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("create game scrape: %w", err)
	}
	if err := buildAndWriteEvidence(ctx, reads, writes, plan, game.PlatformID); err != nil {
		return metadatascrape.ScheduleResult{}, err
	}
	if err := writes.Game(ctx, cmd.GameID, cmd.Version, cmd.Now); err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("advance game scrape version: %w", err)
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return metadatascrape.ScheduleResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return metadatascrape.ScheduleResult{}, fmt.Errorf("commit game scrape scheduling: %w", err)
	}
	return metadatascrape.ScheduleResult{RunID: plan.RunID, JobID: plan.JobID}, nil
}

func scheduleUUID() (string, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create scrape identity: %w", err)
	}
	return value.String(), nil
}

func buildSchedulePlan(
	subject metadatascrape.Subject,
	provider, dedupe string,
	payload map[string]any,
	now int64,
) (metadatascrape.SchedulePlan, error) {
	runID, err := scheduleUUID()
	if err != nil {
		return metadatascrape.SchedulePlan{}, err
	}
	jobID, err := scheduleUUID()
	if err != nil {
		return metadatascrape.SchedulePlan{}, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return metadatascrape.SchedulePlan{}, fmt.Errorf("encode scrape payload: %w", err)
	}
	if subject.Kind == "GAME" {
		dedupe += ":" + runID
	}
	digest := sha256.Sum256([]byte(dedupe))
	plan := metadatascrape.SchedulePlan{
		Subject: subject, RunID: runID, JobID: jobID, Provider: provider,
		Dedupe:      hex.EncodeToString(digest[:]),
		PayloadJSON: string(payloadJSON),
		JobState:    "QUEUED", RunState: "RUNNING",
		EventJSON: fmt.Sprintf(`{"provider":%q}`, provider),
		Now:       now,
	}
	if subject.Kind == "GAME" {
		plan.EventJSON = "{}"
	}
	if provider == "NONE" {
		plan.JobState = "SUCCEEDED"
		plan.RunState = "COMPLETED"
		plan.FinishedAt = &now
	}
	return plan, nil
}

func buildAndWriteEvidence(
	ctx context.Context,
	reads scheduleReads,
	writes scheduleWrites,
	plan metadatascrape.SchedulePlan,
	platform string,
) error {
	var evidence []metadatascrape.HashEvidence
	var err error
	if platform == "arcade" {
		evidence, err = buildArcadeEvidence(ctx, reads, plan)
	} else {
		evidence, err = buildContentEvidence(ctx, reads, plan)
	}
	if err != nil {
		return err
	}
	if err := writes.Evidence(ctx, evidence); err != nil {
		return fmt.Errorf("persist scrape evidence: %w", err)
	}
	return nil
}

func buildContentEvidence(
	ctx context.Context,
	reads scheduleReads,
	plan metadatascrape.SchedulePlan,
) ([]metadatascrape.HashEvidence, error) {
	files, err := reads.Files(ctx, plan.Subject)
	if err != nil {
		return nil, fmt.Errorf("read content hash evidence: %w", err)
	}
	evidence := make([]metadatascrape.HashEvidence, 0, len(files))
	for _, file := range files {
		if strings.EqualFold(filepath.Ext(file.Name), ".zip") && file.ArchiveBlobID == nil {
			continue
		}
		id, err := scheduleUUID()
		if err != nil {
			return nil, err
		}
		item := metadatascrape.HashEvidence{
			ID: id, RunID: plan.RunID, Profile: "RAW_FILE",
			BlobID: &file.BlobID, Hashes: file.Hashes,
			Order: len(evidence), Now: plan.Now,
		}
		if file.ArchiveBlobID != nil && file.ArchiveOrdinal != nil {
			item.Profile = "SINGLE_ARCHIVE_MEMBER"
			item.BlobID = nil
			item.ArchiveBlobID = file.ArchiveBlobID
			item.ArchiveOrdinal = file.ArchiveOrdinal
		}
		evidence = append(evidence, item)
	}
	return evidence, nil
}

func buildArcadeEvidence(
	ctx context.Context,
	reads scheduleReads,
	plan metadatascrape.SchedulePlan,
) ([]metadatascrape.HashEvidence, error) {
	binding, found, err := reads.DAT(ctx, plan.Subject)
	if err != nil {
		return nil, fmt.Errorf("read arcade scrape DAT: %w", err)
	}
	if !found {
		return nil, nil
	}
	var snapshot struct {
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal([]byte(binding.SnapshotJSON), &snapshot); err != nil || snapshot.Machine == "" {
		return nil, metadatascrape.ErrArcadeSnapshotInvalid
	}
	entries, err := reads.Arcade(ctx, plan.Subject, binding.ID, snapshot.Machine)
	if err != nil {
		return nil, fmt.Errorf("read arcade hash evidence: %w", err)
	}
	return selectArcadeEvidence(entries, plan)
}

func selectArcadeEvidence(
	entries []metadatascrape.ArcadeEvidence,
	plan metadatascrape.SchedulePlan,
) ([]metadatascrape.HashEvidence, error) {
	sort.Slice(entries, func(left, right int) bool {
		if (entries[left].SHA1 != nil) != (entries[right].SHA1 != nil) {
			return entries[left].SHA1 != nil
		}
		if entries[left].Size != entries[right].Size {
			return entries[left].Size > entries[right].Size
		}
		return entries[left].Name < entries[right].Name
	})
	seen := make(map[string]struct{}, len(entries))
	evidence := make([]metadatascrape.HashEvidence, 0, 8)
	for _, entry := range entries {
		key := textValue(entry.CRC32) + "\x00" + textValue(entry.SHA1)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		id, err := scheduleUUID()
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, metadatascrape.HashEvidence{
			ID: id, RunID: plan.RunID, Profile: "ARCADE_DAT_ENTRIES",
			ArchiveBlobID:  &entry.ArchiveBlobID,
			ArchiveOrdinal: &entry.Ordinal,
			Hashes:         metadatascrape.Hashes{CRC32: entry.CRC32, SHA1: entry.SHA1},
			Order:          len(evidence), Now: plan.Now,
		})
		if len(evidence) == 8 {
			break
		}
	}
	return evidence, nil
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeReviewRequest(
	ctx context.Context,
	writes scheduleWrites,
	itemID, provider string,
	draft metadatascrape.ReviewSubject,
	result metadatascrape.ScheduleResult,
	actor authn.Actor,
	now int64,
) error {
	before, err := json.Marshal(map[string]any{"schemaVersion": 2, "metadata": json.RawMessage(draft.MetadataJSON)})
	if err != nil {
		return fmt.Errorf("encode review scrape prior state: %w", err)
	}
	after, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "metadataProvider": provider, "scrapeRunId": result.RunID,
	})
	if err != nil {
		return fmt.Errorf("encode review scrape request: %w", err)
	}
	id, err := scheduleUUID()
	if err != nil {
		return err
	}
	err = writes.Review(ctx, metadatascrape.ReviewChange{
		ID: id, ItemID: itemID, BeforeJSON: string(before), AfterJSON: string(after),
		Actor: actor, Version: draft.Version, Now: now,
	})
	if err != nil {
		return fmt.Errorf("record review scrape request: %w", err)
	}
	return nil
}

func (writes scheduleWrites) Create(ctx context.Context, plan metadatascrape.SchedulePlan) error {
	_, err := writes.transaction.ExecContext(ctx, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
 payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,?,?,'METADATA_SCRAPE',?,1,?,1,?,0,4,?,?,?,?)`, plan.JobID, plan.Subject.Kind, plan.Subject.ID, plan.Dedupe,
		plan.PayloadJSON, plan.JobState, plan.Now, plan.FinishedAt, plan.Now, plan.Now)
	if err != nil {
		return fmt.Errorf("insert scrape job: %w", err)
	}
	var itemID, gameID *string
	if plan.Subject.Kind == "GAME" {
		gameID = &plan.Subject.ID
	} else {
		itemID = &plan.Subject.ID
	}
	_, err = writes.transaction.ExecContext(
		ctx,
		`INSERT INTO metadata_scrape_runs
 (id,import_item_id,game_id,job_id,provider,provider_config_version,state,created_at_ms,updated_at_ms,completed_at_ms)
 VALUES(?,?,?,?,?,1,?,?,?,?)`,
		plan.RunID,
		itemID,
		gameID,
		plan.JobID,
		plan.Provider,
		plan.RunState,
		plan.Now,
		plan.Now,
		plan.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("insert scrape run: %w", err)
	}
	_, err = writes.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,?,?,?,?,?)`,
		plan.JobID,
		plan.Subject.Kind,
		plan.Subject.ID,
		plan.JobState,
		plan.EventJSON,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("insert scrape scheduled event: %w", err)
	}
	return nil
}

func (writes scheduleWrites) Evidence(ctx context.Context, evidence []metadatascrape.HashEvidence) error {
	for _, item := range evidence {
		_, err := writes.transaction.ExecContext(ctx, `INSERT INTO content_hash_evidence
 (id,scrape_run_id,profile,blob_id,archive_blob_id,archive_entry_ordinal,
 crc32,md5,sha1,sha256,query_order,created_at_ms)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.RunID, item.Profile, item.BlobID,
			item.ArchiveBlobID, item.ArchiveOrdinal,
			item.CRC32, item.MD5, item.SHA1, item.SHA256, item.Order, item.Now)
		if err != nil {
			return fmt.Errorf("insert content hash evidence: %w", err)
		}
	}
	return nil
}

func (writes scheduleWrites) Game(ctx context.Context, id string, version, now int64) error {
	result, err := recordstore.UpdateGames(ctx, writes.transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`, Values: []any{
			now,
		}, Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args: []any{
				id,
				version,
			},
		},
	})
	return scheduleChanged(result, err, metadatascrape.ErrGameVersionConflict)
}

func (writes scheduleWrites) Review(ctx context.Context, value metadatascrape.ReviewChange) error {
	result, err := recordstore.UpdateReviewDrafts(ctx, writes.transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`, Values: []any{value.Now},
		Scope: recordstore.Scope{Where: `import_item_id=? AND version=?`, Args: []any{value.ItemID, value.Version}},
	})
	if err := scheduleChanged(result, err, metadatascrape.ErrReviewVersionConflict); err != nil {
		return err
	}
	_, err = recordstore.CreateReviewEvents(
		ctx,
		writes.transaction,
		`INSERT INTO review_events
 (id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,diff_json,
 config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
 VALUES(?,?,'SCRAPE_REQUESTED',?,?,?,?,?,?,'{"schemaVersion":2}','{"schemaVersion":2}',?,?)`,

		value.ID,
		value.ItemID,
		value.Actor.Kind,
		value.Actor.UserID,
		value.Actor.Label,
		value.BeforeJSON,
		value.AfterJSON,
		value.AfterJSON,
		value.AfterJSON,
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("insert scrape review event: %w", err)
	}
	return nil
}

func scheduleChanged(result sql.Result, err, errorConflict error) error {
	if err != nil {
		return fmt.Errorf("update scrape subject: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count scrape subject update: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("scrape subject changed: %w", errorConflict)
	}
	return nil
}
