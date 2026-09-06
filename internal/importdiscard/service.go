// Package importdiscard owns the durable, one-way disposition of an import batch.
// It stops existing domain workers and delegates reference release to payloadrelease.
package importdiscard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/emulationstationimport"
	"retrom/internal/libraryimport"
	"retrom/internal/pegasusimport"
)

var ErrInvalid = errors.New("IMPORT_BATCH_DISCARD_INVALID")

const reason = "丢弃本批次未发布内容"

type Status struct {
	Kind      string  `json:"kind"`
	ImportID  string  `json:"importId"`
	State     string  `json:"state"`
	ErrorCode *string `json:"errorCode"`
}

type Service struct {
	database         *sql.DB
	importer         *libraryimport.Service
	pegasus          *pegasusimport.Service
	emulationstation *emulationstationimport.Service
	now              func() time.Time
	stop             chan struct{}
	wait             sync.WaitGroup
}

func New(database *sql.DB, importer *libraryimport.Service, pegasus *pegasusimport.Service,
	emulationstation *emulationstationimport.Service, now func() time.Time,
) *Service {
	return &Service{
		database: database, importer: importer, pegasus: pegasus, emulationstation: emulationstation,
		now: now, stop: make(chan struct{}),
	}
}

func batchTable(kind string) (string, error) {
	switch kind {
	case "IMPORT":
		return "import_jobs", nil
	case "PEGASUS":
		return "pegasus_imports", nil
	case "EMULATIONSTATION":
		return "emulationstation_imports", nil
	default:
		return "", ErrInvalid
	}
}

func (service *Service) Get(ctx context.Context, kind, id string) (Status, error) {
	table, err := batchTable(kind)
	if err != nil {
		return Status{}, err
	}
	if _, err := uuid.Parse(id); err != nil {
		return Status{}, ErrInvalid
	}
	var state string
	var started bool
	startedQuery := "1"
	if kind != "IMPORT" {
		startedQuery = "import_job_id IS NOT NULL"
	}
	if err := service.database.QueryRowContext(ctx, `
SELECT state,`+startedQuery+` FROM `+table+` WHERE id=?`,
		id).
		Scan(&state, &started); err != nil {
		return Status{}, fmt.Errorf("importdiscard/read batch: %w", err)
	}
	result := Status{Kind: kind, ImportID: id, State: "AVAILABLE"}
	available, err := service.hasUndecidedContent(ctx, kind, id)
	if err != nil {
		return Status{}, err
	}
	if !started || !available || state == "SCANNING" || state == "AWAITING_MAPPING" {
		result.State = "UNAVAILABLE"
	}
	var code sql.NullString
	err = service.database.QueryRowContext(ctx, `
SELECT state,error_code FROM import_batch_discards WHERE kind=? AND import_id=?`,
		kind, id).
		Scan(&result.State, &code)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, fmt.Errorf("importdiscard/read disposition: %w", err)
	}
	if code.Valid {
		result.ErrorCode = &code.String
	}
	return result, nil
}

func (service *Service) Request(ctx context.Context, kind, id, userID string) (Status, error) {
	status, err := service.Get(ctx, kind, id)
	if err != nil {
		return Status{}, err
	}
	if status.State == "UNAVAILABLE" {
		return Status{}, ErrInvalid
	}
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Status{}, fmt.Errorf("importdiscard/request: %w", err)
	}
	defer cleanup.Rollback(tx)
	now := service.now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO import_batch_discards
(kind,import_id,requested_by_user_id,state,requested_at_ms,updated_at_ms)
VALUES(?,?,?,'REQUESTED',?,?) ON CONFLICT(kind,import_id) DO UPDATE
SET state='REQUESTED',error_code=NULL,updated_at_ms=excluded.updated_at_ms
WHERE import_batch_discards.state='FAILED'`, kind, id, userID, now, now)
	if err != nil {
		return Status{}, fmt.Errorf("importdiscard/persist request: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Status{}, fmt.Errorf("importdiscard/request result: %w", err)
	}
	if changed > 0 {
		auditID, _ := uuid.NewV7()
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events
(id,actor_kind,actor_user_id,action,resource_type,resource_id,after_json,created_at_ms)
VALUES(?,'USER',?,'IMPORT_BATCH_DISCARD_REQUESTED',?,?,'{"state":"REQUESTED"}',?)`,
			auditID.String(), userID, kind, id, now); err != nil {
			return Status{}, fmt.Errorf("importdiscard/audit request: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Status{}, fmt.Errorf("importdiscard/commit request: %w", err)
	}
	return service.Get(ctx, kind, id)
}

// Start reconciles persisted dispositions, including requests interrupted by a restart.
// Work is bounded; the existing import jobs and PAYLOAD_RELEASE jobs retain their own lifecycle.
func (service *Service) Start() {
	service.wait.Add(1)
	go func() {
		defer service.wait.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-service.stop:
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := service.RunOnce(ctx)
			cleanup.Error("reconcile import batch discard", err)
			cancel()
		}
	}()
}

func (service *Service) Close() { close(service.stop); service.wait.Wait() }

func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	var kind, id, userID string
	err := service.database.QueryRowContext(ctx, `SELECT kind,import_id,requested_by_user_id
FROM import_batch_discards WHERE state='REQUESTED' ORDER BY updated_at_ms,kind,import_id LIMIT 1`).
		Scan(&kind, &id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("importdiscard/pending request: %w", err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: userID})
	done, workErr := service.process(ctx, kind, id, userID)
	state := "REQUESTED"
	var code any
	var completed any
	now := service.now().UnixMilli()
	if done {
		state = "COMPLETED"
		completed = now
	}
	if workErr != nil {
		state = "FAILED"
		code = "IMPORT_BATCH_DISCARD_FAILED"
		if errors.Is(workErr, errReleaseFailed) {
			code = "IMPORT_BATCH_DISCARD_RELEASE_FAILED"
		}
		if errors.Is(workErr, errAmbiguousOwner) {
			code = "IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS"
		}
	}
	_, err = service.database.ExecContext(ctx, `UPDATE import_batch_discards SET state=?,error_code=?,
completed_at_ms=?,updated_at_ms=? WHERE kind=? AND import_id=? AND state='REQUESTED'`,
		state, code, completed, now, kind, id)
	if err != nil {
		return true, fmt.Errorf("importdiscard/update progress: %w", err)
	}
	return true, workErr
}
