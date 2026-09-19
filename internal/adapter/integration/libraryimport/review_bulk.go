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

	libraryimportmodel "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	librarypersistence "retrom/internal/repo/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/security/authn"
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
	Q                        string `json:"q,omitempty"`
	TagID                    string `json:"tagId,omitempty"`
	ImportJobID              string `json:"importJobId,omitempty"`
	PegasusImportID          string `json:"pegasusImportId,omitempty"`
	EmulationStationImportID string `json:"emulationStationImportId,omitempty"`
	PlatformInstanceID       string `json:"platformInstanceId,omitempty"`
	BlockerCode              string `json:"blockerCode,omitempty"`
}

type ReviewBulkCounts struct {
	Matched          int `json:"matched"`
	StrictReady      int `json:"strictReady"`
	ScreenshotOnly   int `json:"screenshotOnly"`
	Duplicate        int `json:"duplicate"`
	AttachmentActive int `json:"attachmentActive"`
	SourceFlagged    int `json:"sourceFlagged"`
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
	itemID, sourceSnapshotID, platformInstanceID, platformName, platformID string
	title, contentKind                                                     string
	reviewVersion, platformVersion                                         int64
	providerID, targetID                                                   sql.NullString
	contentPolicy                                                          contentcapability.Policy
	validationID, validationStatus, dependencySnapshot                     sql.NullString
	validationPlatformVersion                                              sql.NullInt64
	validationDAT, currentDAT, validationDOSEntry, draftDOSEntry           sql.NullString
	screenshotCurrent, attachmentActive, sourceFlagged                     bool
}

func normalizeReviewBulkScope(scope ReviewBulkScope) (ReviewBulkScope, error) {
	scope.Q = strings.ToLower(strings.Join(strings.Fields(scope.Q), " "))
	scope.TagID = strings.TrimSpace(scope.TagID)
	scope.ImportJobID = strings.TrimSpace(scope.ImportJobID)
	scope.PegasusImportID = strings.TrimSpace(scope.PegasusImportID)
	scope.EmulationStationImportID = strings.TrimSpace(scope.EmulationStationImportID)
	scope.PlatformInstanceID = strings.TrimSpace(scope.PlatformInstanceID)
	scope.BlockerCode = strings.TrimSpace(scope.BlockerCode)
	if !utf8.ValidString(scope.Q) || len([]rune(scope.Q)) > 200 || len(scope.BlockerCode) > 120 {
		return ReviewBulkScope{}, ErrReviewBulkInvalidScope
	}
	sourceFilterCount := 0
	for _, value := range []string{scope.ImportJobID, scope.PegasusImportID, scope.EmulationStationImportID} {
		if value != "" {
			sourceFilterCount++
		}
	}
	if sourceFilterCount > 1 {
		return ReviewBulkScope{}, ErrReviewBulkInvalidScope
	}
	for _, value := range []string{
		scope.TagID, scope.ImportJobID, scope.PegasusImportID,
		scope.EmulationStationImportID, scope.PlatformInstanceID,
	} {
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

func scanReviewBulkCandidates(
	ctx context.Context,
	transaction dbexec.Executor,
	scope ReviewBulkScope,
) ([]reviewBulkCandidate, error) {
	rows, err := librarypersistence.BindReviewBulkQueries(transaction).Candidates(
		ctx, libraryimportmodel.ReviewBulkCandidateQuery{
			Scope: libraryimportmodel.ReviewBulkScope{
				Q: scope.Q, TagID: scope.TagID, ImportJobID: scope.ImportJobID,
				PegasusImportID: scope.PegasusImportID, EmulationStationImportID: scope.EmulationStationImportID,
				PlatformInstanceID: scope.PlatformInstanceID, BlockerCode: scope.BlockerCode,
			},
			Limit: reviewBulkMaximumCandidates + 1,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review bulk candidates: %w", err)
	}
	return reviewBulkCandidatesFromApplication(rows), nil
}

func reviewBulkCandidatesFromApplication(rows []libraryimportmodel.ReviewBulkCandidate) []reviewBulkCandidate {
	candidates := make([]reviewBulkCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := reviewBulkCandidate{
			itemID: row.ItemID, reviewVersion: row.ReviewVersion, sourceSnapshotID: row.SourceSnapshotID,
			platformInstanceID: row.PlatformInstanceID, platformName: row.PlatformName, platformID: row.PlatformID,
			platformVersion: row.PlatformVersion, contentPolicy: row.ContentPolicy, contentKind: row.ContentKind,
			screenshotCurrent: row.ScreenshotCurrent, attachmentActive: row.AttachmentActive,
			sourceFlagged: row.SourceFlagged, title: row.Title,
		}
		candidate.providerID = reviewBulkNullableString(row.ProviderID)
		candidate.targetID = reviewBulkNullableString(row.TargetID)
		candidate.validationID = reviewBulkNullableString(row.ValidationID)
		candidate.validationStatus = reviewBulkNullableString(row.ValidationStatus)
		candidate.validationPlatformVersion = reviewBulkNullableInt(row.ValidationPlatformVersion)
		candidate.validationDAT = reviewBulkNullableString(row.ValidationDAT)
		candidate.currentDAT = reviewBulkNullableString(row.CurrentDAT)
		candidate.validationDOSEntry = reviewBulkNullableString(row.ValidationDOSEntry)
		candidate.draftDOSEntry = reviewBulkNullableString(row.DraftDOSEntry)
		candidate.dependencySnapshot = reviewBulkNullableString(row.DependencySnapshot)
		candidates = append(candidates, candidate)
	}
	return candidates
}

func reviewBulkNullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func reviewBulkNullableInt(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func preliminaryQuickApprovalReady(candidate reviewBulkCandidate) bool {
	title := strings.TrimSpace(candidate.title)
	return quickApprovalArtifactReady(candidate) && quickApprovalValidationCurrent(candidate) &&
		nullStringsEqual(candidate.validationDAT, candidate.currentDAT) &&
		nullStringsEqual(candidate.validationDOSEntry, candidate.draftDOSEntry) &&
		candidate.contentPolicy.Supports(candidate.contentKind) &&
		title != "" && validField(title, 200, false)
}

func quickApprovalArtifactReady(candidate reviewBulkCandidate) bool {
	return candidate.providerID.Valid && candidate.targetID.Valid && len(candidate.contentPolicy.SupportedContentKinds) > 0
}

func quickApprovalValidationCurrent(candidate reviewBulkCandidate) bool {
	return candidate.validationID.Valid && candidate.validationStatus.String == "READY"
}

func (service *Service) classifyReviewBulkCandidates(
	ctx context.Context,
	transaction dbexec.Executor,
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
		err := service.validateCurrentApprovalDependencySnapshot(
			ctx, transaction, candidate.sourceSnapshotID, candidate.validationID.String,
			candidate.platformID, candidate.providerID.String, candidate.targetID.String,
			candidate.contentPolicy,
			candidate.contentKind, candidate.dependencySnapshot.String,
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
		if candidate.sourceFlagged {
			counts.SourceFlagged++
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

func activeReviewBulkSummary(
	ctx context.Context,
	transaction dbexec.Executor,
) (ReviewBulkSummary, bool, error) {
	summary, found, err := librarypersistence.BindReviewBulkQueries(transaction).ActiveSummary(ctx)
	if err != nil {
		return ReviewBulkSummary{}, false, fmt.Errorf("libraryimport/review bulk active: %w", err)
	}
	if !found {
		return ReviewBulkSummary{}, false, nil
	}
	return reviewBulkSummaryFromApplication(summary), true, nil
}

func (service *Service) reviewBulkPreviewInTransaction(
	ctx context.Context,
	transaction dbexec.Executor,
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
	transaction dbexec.Executor,
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
	created, err := librarypersistence.BindReviewBulkWrites(transaction).Create(ctx, libraryimportmodel.ReviewBulkCreation{
		BulkApprovalID: bulkID.String(), JobID: jobID.String(), CreatedByUserID: createdBy,
		Scope: libraryimportmodel.ReviewBulkScope{
			Q: preview.Scope.Q, TagID: preview.Scope.TagID, ImportJobID: preview.Scope.ImportJobID,
			PegasusImportID: preview.Scope.PegasusImportID, EmulationStationImportID: preview.Scope.EmulationStationImportID,
			PlatformInstanceID: preview.Scope.PlatformInstanceID, BlockerCode: preview.Scope.BlockerCode,
		}, ScopeJSON: scopeJSON, ScopeDigest: preview.ScopeDigest,
		CandidateManifestDigest: preview.CandidateManifestDigest, PayloadJSON: string(payload),
		DedupeKey: hex.EncodeToString(dedupe[:]), InputDigest: hex.EncodeToString(inputDigest[:]),
		Counts: libraryimportmodel.ReviewBulkCounts{
			Matched: preview.Counts.Matched, StrictReady: preview.Counts.StrictReady,
			ScreenshotOnly: preview.Counts.ScreenshotOnly, Duplicate: preview.Counts.Duplicate,
			AttachmentActive: preview.Counts.AttachmentActive, SourceFlagged: preview.Counts.SourceFlagged,
			NotReadyOrStale: preview.Counts.NotReadyOrStale,
		}, Candidates: reviewBulkCreationCandidates(candidates), NowMS: now,
	})
	if err != nil {
		if strings.Contains(err.Error(), "review_bulk_approvals_one_active") {
			return ReviewBulkSummary{}, ErrReviewBulkActive
		}
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create: %w", err)
	}
	return reviewBulkSummaryFromApplication(created), nil
}

func reviewBulkCreationCandidates(candidates []reviewBulkCandidate) []libraryimportmodel.ReviewBulkCandidate {
	result := make([]libraryimportmodel.ReviewBulkCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, libraryimportmodel.ReviewBulkCandidate{
			ItemID: candidate.itemID, SourceSnapshotID: candidate.sourceSnapshotID,
			PlatformInstanceID: candidate.platformInstanceID, PlatformName: candidate.platformName,
			PlatformID: candidate.platformID, Title: strings.TrimSpace(candidate.title),
			ContentKind: candidate.contentKind, ReviewVersion: candidate.reviewVersion,
			PlatformVersion: candidate.platformVersion, ProviderID: nullableStringPointer(candidate.providerID),
			TargetID: nullableStringPointer(candidate.targetID), ContentPolicy: candidate.contentPolicy,
			ValidationID:              nullableStringPointer(candidate.validationID),
			ValidationStatus:          nullableStringPointer(candidate.validationStatus),
			ValidationPlatformVersion: nullableInt64Pointer(candidate.validationPlatformVersion),
			ValidationDAT:             nullableStringPointer(candidate.validationDAT),
			CurrentDAT:                nullableStringPointer(candidate.currentDAT),
			ValidationDOSEntry:        nullableStringPointer(candidate.validationDOSEntry),
			DraftDOSEntry:             nullableStringPointer(candidate.draftDOSEntry),
			DependencySnapshot:        nullableStringPointer(candidate.dependencySnapshot),
			ScreenshotCurrent:         candidate.screenshotCurrent, AttachmentActive: candidate.attachmentActive,
			SourceFlagged: candidate.sourceFlagged,
		})
	}
	return result
}

func (service *Service) PreviewReviewBulk(ctx context.Context, scope ReviewBulkScope) (ReviewBulkPreview, error) {
	var preview ReviewBulkPreview
	err := librarypersistence.NewTransactions(service.database).Read(ctx, func(executor dbexec.Executor) error {
		var err error
		preview, _, err = service.reviewBulkPreviewInTransaction(ctx, executor, scope)
		return err
	})
	if err != nil {
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
	var created ReviewBulkSummary
	err := librarypersistence.NewTransactions(service.database).Write(ctx, func(executor dbexec.Executor) error {
		preview, candidates, err := service.reviewBulkPreviewInTransaction(ctx, executor, request.Scope)
		if err != nil {
			return err
		}
		if err := validateReviewBulkCreate(preview, candidates, request); err != nil {
			return err
		}
		scopeJSON, _, err := reviewBulkScopeDigest(preview.Scope)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		created, err = insertReviewBulkRecords(
			ctx, executor, principal.UserID, preview, candidates, scopeJSON, now,
		)
		return err
	})
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk create: %w", err)
	}
	go service.runReviewBulkApproval(context.WithoutCancel(ctx), created.BulkApprovalID)
	return created, nil
}

func (service *Service) GetReviewBulk(ctx context.Context, bulkID string) (ReviewBulkSummary, error) {
	summary, err := librarypersistence.BindReviewBulkQueries(service.database).Summary(ctx, bulkID)
	if err != nil {
		if errors.Is(err, libraryimportmodel.ErrReviewBulkQuery) {
			return ReviewBulkSummary{}, ErrReviewBulkConflict
		}
		return ReviewBulkSummary{}, fmt.Errorf("libraryimport/review bulk get: %w", err)
	}
	return reviewBulkSummaryFromApplication(summary), nil
}

func reviewBulkSummaryFromApplication(summary libraryimportmodel.ReviewBulkSummary) ReviewBulkSummary {
	return ReviewBulkSummary{
		BulkApprovalID: summary.BulkApprovalID, JobID: summary.JobID, State: summary.State,
		Version: summary.Version,
		Scope: ReviewBulkScope{
			Q: summary.Scope.Q, TagID: summary.Scope.TagID, ImportJobID: summary.Scope.ImportJobID,
			PegasusImportID:          summary.Scope.PegasusImportID,
			EmulationStationImportID: summary.Scope.EmulationStationImportID,
			PlatformInstanceID:       summary.Scope.PlatformInstanceID, BlockerCode: summary.Scope.BlockerCode,
		},
		Counts: ReviewBulkCounts{
			Matched: summary.Counts.Matched, StrictReady: summary.Counts.StrictReady,
			ScreenshotOnly: summary.Counts.ScreenshotOnly, Duplicate: summary.Counts.Duplicate,
			AttachmentActive: summary.Counts.AttachmentActive, SourceFlagged: summary.Counts.SourceFlagged,
			NotReadyOrStale: summary.Counts.NotReadyOrStale,
		},
		Progress: ReviewBulkProgress{
			Candidate: summary.Progress.Candidate, Processed: summary.Progress.Processed,
			Published: summary.Progress.Published, SkippedDuplicate: summary.Progress.SkippedDuplicate,
			SkippedChanged: summary.Progress.SkippedChanged, SkippedNotReady: summary.Progress.SkippedNotReady,
			Failed: summary.Progress.Failed, Cancelled: summary.Progress.Cancelled,
		},
		CreatedAtMS: summary.CreatedAtMS, StartedAtMS: summary.StartedAtMS,
		UpdatedAtMS: summary.UpdatedAtMS, CompletedAtMS: summary.CompletedAtMS,
		LastErrorCode: summary.LastErrorCode,
	}
}
