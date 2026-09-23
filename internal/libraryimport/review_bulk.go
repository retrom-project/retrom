package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/contentcapability"
	"retrom/internal/dbexec"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

var (
	ErrReviewBulkInvalidScope = errors.New("REVIEW_BULK_INVALID_SCOPE")
	ErrReviewBulkActive       = errors.New("REVIEW_BULK_APPROVAL_ACTIVE")
	ErrReviewBulkTooLarge     = errors.New("REVIEW_BULK_SCOPE_TOO_LARGE")
	ErrReviewBulkEmpty        = errors.New("REVIEW_BULK_SCOPE_EMPTY")
	ErrReviewBulkConflict     = errors.New("REVIEW_BULK_VERSION_CONFLICT")
)

type ReviewBulkSummary = libraryservice.ReviewBulkSummary

type ReviewBulkCounts struct {
	Matched, StrictReady, ScreenshotOnly, Duplicate, AttachmentActive, SourceFlagged, NotReadyOrStale int
}

type ReviewBulkScope struct {
	Q                  string `json:"q,omitempty"`
	TagID              string `json:"tagId,omitempty"`
	ImportJobID        string `json:"importJobId,omitempty"`
	SourceImportID     string `json:"sourceImportId,omitempty"`
	PlatformInstanceID string `json:"platformInstanceId,omitempty"`
	BlockerCode        string `json:"blockerCode,omitempty"`
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
	scope.SourceImportID = strings.TrimSpace(scope.SourceImportID)
	scope.PlatformInstanceID = strings.TrimSpace(scope.PlatformInstanceID)
	scope.BlockerCode = strings.TrimSpace(scope.BlockerCode)
	if !utf8.ValidString(scope.Q) || len([]rune(scope.Q)) > 200 || len(scope.BlockerCode) > 120 {
		return ReviewBulkScope{}, ErrReviewBulkInvalidScope
	}
	sourceFilterCount := 0
	for _, value := range []string{scope.ImportJobID, scope.SourceImportID} {
		if value != "" {
			sourceFilterCount++
		}
	}
	if sourceFilterCount > 1 {
		return ReviewBulkScope{}, ErrReviewBulkInvalidScope
	}
	for _, value := range []string{
		scope.TagID, scope.ImportJobID, scope.SourceImportID,
		scope.PlatformInstanceID,
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

func nullStringsEqual(left, right sql.NullString) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func reviewBulkCandidatesFromApplication(rows []libraryservice.ReviewBulkCandidate) []reviewBulkCandidate {
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

func (service *Service) CreateReviewBulk(ctx context.Context) (ReviewBulkSummary, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	bulkID, err := uuid.NewV7()
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("allocate bulk approval: %w", err)
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("allocate bulk job: %w", err)
	}
	now := service.now().UnixMilli()
	var created ReviewBulkSummary
	err = librarypersistence.NewTransactions(service.database).Write(ctx, func(executor dbexec.Executor) error {
		repository := librarypersistence.BindReviewBulkWrites(executor)
		var createErr error
		created, createErr = repository.CreateGlobal(ctx, bulkID.String(), jobID.String(), principal.UserID, now)
		if createErr != nil {
			return fmt.Errorf("create bounded global review: %w", createErr)
		}
		return nil
	})
	if err != nil {
		_, active, readErr := librarypersistence.NewReviewBulkQueries(service.database).ActiveSummary(ctx)
		if readErr == nil && active {
			return ReviewBulkSummary{}, ErrReviewBulkActive
		}
		if errors.Is(err, librarypersistence.ErrReviewBulkEmpty) {
			return ReviewBulkSummary{}, ErrReviewBulkEmpty
		}
		if errors.Is(err, librarypersistence.ErrReviewBulkTooLarge) {
			return ReviewBulkSummary{}, ErrReviewBulkTooLarge
		}
		return ReviewBulkSummary{}, fmt.Errorf("create review bulk: %w", err)
	}
	go service.runReviewBulkApproval(context.WithoutCancel(ctx), created.BulkApprovalID)
	return created, nil
}

func (service *Service) GetReviewBulk(ctx context.Context, bulkID string) (ReviewBulkSummary, error) {
	if _, err := uuid.Parse(bulkID); err != nil {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	summary, err := librarypersistence.NewReviewBulkQueries(service.database).Summary(ctx, bulkID)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("read review bulk: %w", err)
	}
	return summary, nil
}

func (service *Service) GetActiveReviewBulk(ctx context.Context) (ReviewBulkSummary, bool, error) {
	result, found, err := librarypersistence.NewReviewBulkQueries(service.database).ActiveSummary(ctx)
	if err != nil {
		return ReviewBulkSummary{}, false, fmt.Errorf("read active review bulk: %w", err)
	}
	return result, found, nil
}
