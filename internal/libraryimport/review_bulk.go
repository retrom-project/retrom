package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
)

const (
	reviewBulkMaximumCandidates = 10_000
	reviewBulkDeadline          = time.Hour
)

var (
	ErrReviewBulkInvalidScope = errors.New("REVIEW_BULK_INVALID_SCOPE")
	ErrReviewBulkPreviewStale = errors.New("REVIEW_BULK_PREVIEW_STALE")
	ErrReviewBulkActive       = errors.New("REVIEW_BULK_APPROVAL_ACTIVE")
	ErrReviewBulkTooLarge     = errors.New("REVIEW_BULK_SCOPE_TOO_LARGE")
	ErrReviewBulkEmpty        = errors.New("REVIEW_BULK_SCOPE_EMPTY")
	ErrReviewBulkConflict     = errors.New("REVIEW_BULK_VERSION_CONFLICT")
)

type ReviewBulkScope struct {
	Q                  string `json:"q,omitempty"`
	TagID              string `json:"tagId,omitempty"`
	ImportJobID        string `json:"importJobId,omitempty"`
	PegasusImportID    string `json:"pegasusImportId,omitempty"`
	PlatformInstanceID string `json:"platformInstanceId,omitempty"`
	BlockerCode        string `json:"blockerCode,omitempty"`
}

type ReviewBulkCounts struct {
	Matched          int `json:"matched"`
	StrictReady      int `json:"strictReady"`
	ScreenshotOnly   int `json:"screenshotOnly"`
	Duplicate        int `json:"duplicate"`
	AttachmentActive int `json:"attachmentActive"`
	NotReadyOrStale  int `json:"notReadyOrStale"`
}

type ReviewBulkProgress struct {
	Candidate        int `json:"candidate"`
	Processed        int `json:"processed"`
	Published        int `json:"published"`
	SkippedDuplicate int `json:"skippedDuplicate"`
	SkippedChanged   int `json:"skippedChanged"`
	SkippedNotReady  int `json:"skippedNotReady"`
	Failed           int `json:"failed"`
	Cancelled        int `json:"cancelled"`
}

type ReviewBulkSummary struct {
	BulkApprovalID string             `json:"bulkApprovalId"`
	JobID          string             `json:"jobId"`
	State          string             `json:"state"`
	Version        int64              `json:"version"`
	Scope          ReviewBulkScope    `json:"scope"`
	Counts         ReviewBulkCounts   `json:"initialCounts"`
	Progress       ReviewBulkProgress `json:"counts"`
	CreatedAtMS    int64              `json:"createdAtMs"`
	StartedAtMS    *int64             `json:"startedAtMs"`
	UpdatedAtMS    int64              `json:"updatedAtMs"`
	CompletedAtMS  *int64             `json:"completedAtMs"`
	LastErrorCode  *string            `json:"lastErrorCode"`
}

type ReviewBulkPreview struct {
	GeneratedAtMS           int64              `json:"generatedAtMs"`
	Scope                   ReviewBulkScope    `json:"scope"`
	ScopeDigest             string             `json:"scopeDigest"`
	CandidateManifestDigest string             `json:"candidateManifestDigest"`
	Counts                  ReviewBulkCounts   `json:"counts"`
	ActiveBulkApproval      *ReviewBulkSummary `json:"activeBulkApproval"`
}

type ReviewBulkCreateRequest struct {
	Scope                   ReviewBulkScope `json:"scope"`
	ScopeDigest             string          `json:"scopeDigest"`
	CandidateManifestDigest string          `json:"candidateManifestDigest"`
}

type ReviewBulkItemResult struct {
	ImportItemID   string  `json:"importItemId"`
	Title          string  `json:"title"`
	PlatformName   string  `json:"platformName"`
	State          string  `json:"state"`
	GameID         *string `json:"gameId"`
	ReviewEventID  *string `json:"reviewEventId"`
	OutcomeCode    *string `json:"outcomeCode"`
	OutcomeDetails any     `json:"outcomeDetails"`
	CompletedAtMS  *int64  `json:"completedAtMs"`
}

type reviewBulkCandidate struct {
	itemID, sourceSnapshotID, platformInstanceID, platformName, platformID     string
	title, contentKind                                                         string
	reviewVersion, platformVersion                                             int64
	artifactID, artifactCompatibility                                          sql.NullString
	artifactVersion                                                            sql.NullInt64
	validationID, validationStatus, dependencySnapshot                         sql.NullString
	validationGeneration, validationPlatformVersion, validationArtifactVersion sql.NullInt64
	validationDAT, currentDAT, validationDOSEntry, draftDOSEntry               sql.NullString
	screenshotCurrent, attachmentActive                                        bool
}

func normalizeReviewBulkScope(scope ReviewBulkScope) (ReviewBulkScope, error) {
	scope.Q = strings.ToLower(strings.Join(strings.Fields(scope.Q), " "))
	scope.TagID = strings.TrimSpace(scope.TagID)
	scope.ImportJobID = strings.TrimSpace(scope.ImportJobID)
	scope.PegasusImportID = strings.TrimSpace(scope.PegasusImportID)
	scope.PlatformInstanceID = strings.TrimSpace(scope.PlatformInstanceID)
	scope.BlockerCode = strings.TrimSpace(scope.BlockerCode)
	if !utf8.ValidString(scope.Q) || len([]rune(scope.Q)) > 200 || len(scope.BlockerCode) > 120 {
		return ReviewBulkScope{}, ErrReviewBulkInvalidScope
	}
	for _, value := range []string{scope.TagID, scope.ImportJobID, scope.PegasusImportID, scope.PlatformInstanceID} {
		if value == "" {
			continue
		}
		if _, err := uuid.Parse(value); err != nil {
			return ReviewBulkScope{}, ErrReviewBulkInvalidScope
		}
	}
	return scope, nil
}

func reviewBulkScopeDigest(scope ReviewBulkScope) (string, string, error) {
	encoded, err := json.Marshal(scope)
	if err != nil {
		return "", "", fmt.Errorf("libraryimport/review bulk scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:]), nil
}

func nullStringsEqual(left, right sql.NullString) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func reviewBulkCandidatesQuery(scope ReviewBulkScope) (string, []any) {
	query := `
SELECT item.id,draft.version,draft.effective_source_snapshot_id,
       json_extract(draft.metadata_json,'$.title'),instance.id,instance.name,instance.platform_id,instance.version,
       artifact.id,artifact.compatibility_config_json,artifact.version,
       validation.id,validation.status,validation.prepublish_generation,
       validation.platform_instance_version,validation.core_artifact_version,
       validation.dat_version_id,
       (SELECT active.id FROM dat_versions active WHERE active.core_artifact_id=artifact.id AND active.is_active=1),
       validation.default_dos_entry,draft.default_dos_entry,validation.dependency_snapshot_json,
       source.content_kind,
       EXISTS(SELECT 1 FROM review_runtime_screenshots screenshot
         WHERE screenshot.import_item_id=item.id AND screenshot.validation_id=validation.id
         AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
         AND screenshot.core_artifact_id=validation.core_artifact_id),
       EXISTS(SELECT 1 FROM review_arcade_parent_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING')) OR
       EXISTS(SELECT 1 FROM review_multidisc_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING'))
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots source ON source.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
LEFT JOIN core_artifacts artifact ON artifact.core_id=instance.default_core_id AND artifact.enabled=1
LEFT JOIN import_item_core_validations validation ON validation.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  AND candidate.core_artifact_id=artifact.id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
LEFT JOIN pegasus_import_items pegasus ON pegasus.library_import_item_id=item.id
WHERE item.state='REVIEW_PENDING'
AND (pegasus.id IS NULL OR pegasus.execution_state='REVIEW_PENDING')`
	arguments := make([]any, 0, 8)
	if scope.ImportJobID != "" {
		query += " AND item.import_job_id=?"
		arguments = append(arguments, scope.ImportJobID)
	}
	if scope.PegasusImportID != "" {
		query += " AND pegasus.import_id=?"
		arguments = append(arguments, scope.PegasusImportID)
	}
	if scope.Q != "" {
		query += ` AND (instr(item.search_text,?)>0 OR EXISTS(
  SELECT 1 FROM review_draft_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
  WHERE relation.review_draft_id=draft.id AND instr(tag.name_key,?)>0))`
		arguments = append(arguments, scope.Q, scope.Q)
	}
	if scope.TagID != "" {
		query += ` AND EXISTS(SELECT 1 FROM review_draft_tags relation
  JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
  WHERE relation.review_draft_id=draft.id AND tag.id=?)`
		arguments = append(arguments, scope.TagID)
	}
	if scope.PlatformInstanceID != "" {
		query += " AND draft.target_platform_instance_id=?"
		arguments = append(arguments, scope.PlatformInstanceID)
	}
	if scope.BlockerCode != "" {
		query += " AND (validation.compatibility_code=? OR (?='NEEDS_VALIDATION' AND validation.id IS NULL))"
		arguments = append(arguments, scope.BlockerCode, scope.BlockerCode)
	}
	query += " ORDER BY item.id"
	return query, arguments
}

func scanReviewBulkCandidates(
	ctx context.Context,
	transaction *sql.Tx,
	scope ReviewBulkScope,
) ([]reviewBulkCandidate, error) {
	query, arguments := reviewBulkCandidatesQuery(scope)
	rows, err := transaction.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review bulk candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	candidates := make([]reviewBulkCandidate, 0)
	for rows.Next() {
		var candidate reviewBulkCandidate
		if err := rows.Scan(
			&candidate.itemID, &candidate.reviewVersion, &candidate.sourceSnapshotID,
			&candidate.title, &candidate.platformInstanceID, &candidate.platformName, &candidate.platformID,
			&candidate.platformVersion,
			&candidate.artifactID, &candidate.artifactCompatibility, &candidate.artifactVersion,
			&candidate.validationID, &candidate.validationStatus, &candidate.validationGeneration,
			&candidate.validationPlatformVersion, &candidate.validationArtifactVersion,
			&candidate.validationDAT, &candidate.currentDAT, &candidate.validationDOSEntry,
			&candidate.draftDOSEntry, &candidate.dependencySnapshot, &candidate.contentKind,
			&candidate.screenshotCurrent, &candidate.attachmentActive,
		); err != nil {
			return nil, fmt.Errorf("libraryimport/review bulk candidates: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/review bulk candidates: %w", err)
	}
	return candidates, nil
}

func preliminaryQuickApprovalReady(candidate reviewBulkCandidate) bool {
	title := strings.TrimSpace(candidate.title)
	return quickApprovalArtifactReady(candidate) && quickApprovalValidationCurrent(candidate) &&
		nullStringsEqual(candidate.validationDAT, candidate.currentDAT) &&
		nullStringsEqual(candidate.validationDOSEntry, candidate.draftDOSEntry) &&
		contentcapability.SupportsContentKind(candidate.artifactCompatibility.String, candidate.contentKind) &&
		title != "" && validField(title, 200, false)
}

func quickApprovalArtifactReady(candidate reviewBulkCandidate) bool {
	return candidate.artifactID.Valid && candidate.artifactCompatibility.Valid && candidate.artifactVersion.Valid
}

func quickApprovalValidationCurrent(candidate reviewBulkCandidate) bool {
	return candidate.validationID.Valid && candidate.validationStatus.String == "READY" &&
		candidate.validationGeneration.Valid && candidate.validationGeneration.Int64 == prepublishGeneration &&
		candidate.validationPlatformVersion.Valid && candidate.validationArtifactVersion.Valid &&
		candidate.validationPlatformVersion.Int64 == candidate.platformVersion &&
		candidate.validationArtifactVersion.Int64 == candidate.artifactVersion.Int64
}

func (service *Service) classifyReviewBulkCandidates(
	ctx context.Context,
	transaction *sql.Tx,
	matched []reviewBulkCandidate,
) ([]reviewBulkCandidate, ReviewBulkCounts, error) {
	counts := ReviewBulkCounts{Matched: len(matched)}
	qualified := make([]reviewBulkCandidate, 0, len(matched))
	for _, candidate := range matched {
		switch {
		case candidate.attachmentActive:
			counts.AttachmentActive++
			continue
		case candidate.validationStatus.String != "READY" && candidate.screenshotCurrent:
			counts.ScreenshotOnly++
			continue
		case !preliminaryQuickApprovalReady(candidate):
			counts.NotReadyOrStale++
			continue
		}
		snapshot, err := corevalidation.ParseSnapshot(candidate.dependencySnapshot.String)
		if err != nil {
			counts.NotReadyOrStale++
			continue
		}
		err = validateCurrentApprovalSnapshot(
			ctx, transaction, candidate.sourceSnapshotID, candidate.validationID.String,
			candidate.platformID, candidate.artifactID.String, candidate.artifactCompatibility.String,
			candidate.contentKind, snapshot, candidate.dependencySnapshot.String,
		)
		if errors.Is(err, ErrInvalid) {
			counts.NotReadyOrStale++
			continue
		}
		if err != nil {
			return nil, ReviewBulkCounts{}, err
		}
		duplicates, err := findDuplicateGames(ctx, transaction, candidate.itemID, candidate.platformID)
		if err != nil {
			return nil, ReviewBulkCounts{}, fmt.Errorf("libraryimport/review bulk duplicates: %w", err)
		}
		if len(duplicates) != 0 {
			counts.Duplicate++
			continue
		}
		qualified = append(qualified, candidate)
	}
	counts.StrictReady = len(qualified)
	return qualified, counts, nil
}

func reviewBulkManifestDigest(candidates []reviewBulkCandidate) string {
	ordered := append([]reviewBulkCandidate(nil), candidates...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].itemID < ordered[right].itemID })
	digest := sha256.New()
	for _, candidate := range ordered {
		_, _ = fmt.Fprintf(
			digest, "%s\x00%d\x00%s\x00%s\n", candidate.itemID, candidate.reviewVersion,
			candidate.validationID.String, candidate.sourceSnapshotID,
		)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func scanReviewBulkSummary(scanner interface{ Scan(...any) error }) (ReviewBulkSummary, error) {
	var summary ReviewBulkSummary
	var scopeJSON string
	var startedAt, completedAt sql.NullInt64
	var lastError sql.NullString
	err := scanner.Scan(
		&summary.BulkApprovalID, &summary.JobID, &summary.State, &summary.Version, &scopeJSON,
		&summary.Counts.Matched, &summary.Counts.StrictReady, &summary.Counts.ScreenshotOnly,
		&summary.Counts.Duplicate, &summary.Counts.AttachmentActive, &summary.Counts.NotReadyOrStale,
		&summary.Progress.Candidate, &summary.Progress.Processed, &summary.Progress.Published,
		&summary.Progress.SkippedDuplicate, &summary.Progress.SkippedChanged,
		&summary.Progress.SkippedNotReady, &summary.Progress.Failed, &summary.Progress.Cancelled,
		&summary.CreatedAtMS, &startedAt, &summary.UpdatedAtMS, &completedAt, &lastError,
	)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk summary: %w", err)
	}
	if err := json.Unmarshal([]byte(scopeJSON), &summary.Scope); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk scope projection: %w", err)
	}
	summary.StartedAtMS = nullableInt64Pointer(startedAt)
	summary.CompletedAtMS = nullableInt64Pointer(completedAt)
	summary.LastErrorCode = nullableStringPointer(lastError)
	return summary, nil
}

const reviewBulkSummarySelect = `
SELECT bulk.id,bulk.job_id,bulk.state,bulk.version,bulk.scope_json,
       bulk.matched_count,bulk.candidate_count,bulk.screenshot_only_count,
       bulk.duplicate_count,bulk.attachment_active_count,bulk.not_ready_or_stale_count,
       bulk.candidate_count,bulk.processed_count,bulk.published_count,
       bulk.skipped_duplicate_count,bulk.skipped_changed_count,bulk.skipped_not_ready_count,
       bulk.failed_count,bulk.cancelled_count,bulk.created_at_ms,bulk.started_at_ms,
       bulk.updated_at_ms,bulk.completed_at_ms,bulk.last_error_code
FROM review_bulk_approvals bulk`

func activeReviewBulkSummary(
	ctx context.Context,
	transaction *sql.Tx,
) (ReviewBulkSummary, bool, error) {
	summary, err := scanReviewBulkSummary(transaction.QueryRowContext(
		ctx, reviewBulkSummarySelect+" WHERE bulk.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') LIMIT 1",
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewBulkSummary{}, false, nil
	}
	if err != nil {
		return ReviewBulkSummary{}, false, fmt.Errorf("libraryimport/review bulk active: %w", err)
	}
	return summary, true, nil
}

func (service *Service) reviewBulkPreviewInTransaction(
	ctx context.Context,
	transaction *sql.Tx,
	scope ReviewBulkScope,
) (ReviewBulkPreview, []reviewBulkCandidate, error) {
	normalized, err := normalizeReviewBulkScope(scope)
	if err != nil {
		return ReviewBulkPreview{}, nil, err
	}
	_, scopeDigest, err := reviewBulkScopeDigest(normalized)
	if err != nil {
		return ReviewBulkPreview{}, nil, err
	}
	matched, err := scanReviewBulkCandidates(ctx, transaction, normalized)
	if err != nil {
		return ReviewBulkPreview{}, nil, err
	}
	qualified, counts, err := service.classifyReviewBulkCandidates(ctx, transaction, matched)
	if err != nil {
		return ReviewBulkPreview{}, nil, err
	}
	active, hasActive, err := activeReviewBulkSummary(ctx, transaction)
	if err != nil {
		return ReviewBulkPreview{}, nil, err
	}
	var activePointer *ReviewBulkSummary
	if hasActive {
		activePointer = &active
	}
	return ReviewBulkPreview{
		GeneratedAtMS: service.now().UnixMilli(), Scope: normalized, ScopeDigest: scopeDigest,
		CandidateManifestDigest: reviewBulkManifestDigest(qualified), Counts: counts, ActiveBulkApproval: activePointer,
	}, qualified, nil
}

func validateReviewBulkCreate(
	preview ReviewBulkPreview,
	candidates []reviewBulkCandidate,
	request ReviewBulkCreateRequest,
) error {
	if preview.ActiveBulkApproval != nil {
		return ErrReviewBulkActive
	}
	if preview.ScopeDigest != request.ScopeDigest ||
		preview.CandidateManifestDigest != request.CandidateManifestDigest {
		return ErrReviewBulkPreviewStale
	}
	if len(candidates) == 0 {
		return ErrReviewBulkEmpty
	}
	if len(candidates) > reviewBulkMaximumCandidates {
		return ErrReviewBulkTooLarge
	}
	return nil
}

func insertReviewBulkRecords(
	ctx context.Context,
	transaction *sql.Tx,
	createdBy string,
	preview ReviewBulkPreview,
	candidates []reviewBulkCandidate,
	scopeJSON string,
	now int64,
) (ReviewBulkSummary, error) {
	bulkID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "bulkApprovalId": bulkID.String(), "scopeDigest": preview.ScopeDigest,
		"candidateManifestDigest": preview.CandidateManifestDigest, "candidateCount": len(candidates),
	})
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk payload: %w", err)
	}
	dedupe := sha256.Sum256([]byte(bulkID.String()))
	inputDigest := sha256.Sum256(payload)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'REVIEW_BULK_APPROVE',?,1,?,1,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), bulkID.String(), hex.EncodeToString(dedupe[:]), string(payload), now, now, now); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, jobID.String(), string(payload), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'QUEUED',json_object('candidateCount',?),?)
`, jobID.String(), bulkID.String(), len(candidates), now); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_bulk_approvals(id,job_id,state,scope_json,scope_digest,candidate_manifest_digest,
matched_count,candidate_count,screenshot_only_count,duplicate_count,attachment_active_count,
not_ready_or_stale_count,created_by_user_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,'QUEUED',?,?,?,?,?,?,?,?,?,?,1,?,?)
`,
		bulkID.String(), jobID.String(), scopeJSON, preview.ScopeDigest, preview.CandidateManifestDigest,
		preview.Counts.Matched, len(candidates), preview.Counts.ScreenshotOnly, preview.Counts.Duplicate,
		preview.Counts.AttachmentActive, preview.Counts.NotReadyOrStale, createdBy, now, now,
	); err != nil {
		if strings.Contains(err.Error(), "review_bulk_approvals_one_active") {
			return ReviewBulkSummary{}, ErrReviewBulkActive
		}
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create: %w", err)
	}
	for ordinal, candidate := range candidates {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_bulk_approval_items(bulk_approval_id,import_item_id,ordinal,expected_review_version,
expected_validation_id,expected_source_snapshot_id,title_snapshot,target_platform_instance_id,
target_platform_name_snapshot,state,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'PENDING',?)
`, bulkID.String(), candidate.itemID, ordinal, candidate.reviewVersion, candidate.validationID.String,
			candidate.sourceSnapshotID, strings.TrimSpace(candidate.title), candidate.platformInstanceID,
			candidate.platformName, now); err != nil {
			return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk item: %w", err)
		}
	}
	return ReviewBulkSummary{
		BulkApprovalID: bulkID.String(), JobID: jobID.String(), State: "QUEUED", Version: 1,
		Scope: preview.Scope, Counts: preview.Counts,
		Progress: ReviewBulkProgress{Candidate: len(candidates)}, CreatedAtMS: now, UpdatedAtMS: now,
	}, nil
}

func (service *Service) PreviewReviewBulk(ctx context.Context, scope ReviewBulkScope) (ReviewBulkPreview, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ReviewBulkPreview{}, fmt.Errorf("libraryimport/review bulk preview: %w", err)
	}
	defer cleanup.Rollback(transaction)
	preview, _, err := service.reviewBulkPreviewInTransaction(ctx, transaction, scope)
	if err != nil {
		return ReviewBulkPreview{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewBulkPreview{}, fmt.Errorf("libraryimport/review bulk preview: %w", err)
	}
	return preview, nil
}

func (service *Service) CreateReviewBulk(
	ctx context.Context,
	request ReviewBulkCreateRequest,
) (ReviewBulkSummary, error) {
	principal, authenticated := authn.PrincipalFromContext(ctx)
	if !authenticated || principal.UserID == "" {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create: %w", err)
	}
	defer cleanup.Rollback(transaction)
	preview, candidates, err := service.reviewBulkPreviewInTransaction(ctx, transaction, request.Scope)
	if err != nil {
		return ReviewBulkSummary{}, err
	}
	if err := validateReviewBulkCreate(preview, candidates, request); err != nil {
		return ReviewBulkSummary{}, err
	}
	scopeJSON, _, err := reviewBulkScopeDigest(preview.Scope)
	if err != nil {
		return ReviewBulkSummary{}, err
	}
	now := service.now().UnixMilli()
	created, err := insertReviewBulkRecords(
		ctx, transaction, principal.UserID, preview, candidates, scopeJSON, now,
	)
	if err != nil {
		return ReviewBulkSummary{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create: %w", err)
	}
	go service.runReviewBulkApproval(context.WithoutCancel(ctx), created.BulkApprovalID)
	return created, nil
}

func (service *Service) GetReviewBulk(ctx context.Context, bulkID string) (ReviewBulkSummary, error) {
	if _, err := uuid.Parse(bulkID); err != nil {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	summary, err := scanReviewBulkSummary(service.database.QueryRowContext(
		ctx, reviewBulkSummarySelect+" WHERE bulk.id=?", bulkID,
	))
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk get: %w", err)
	}
	return summary, nil
}
