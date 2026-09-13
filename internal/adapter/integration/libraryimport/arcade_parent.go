package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	librarypersistence "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"

	"retrom/internal/capability/content/contentcapability"

	"github.com/google/uuid"
)

const parentAttachmentDeadline = application.ArcadeParentAttachmentDeadline

const (
	ParentErrorInvalid       = "REVIEW_PARENT_UPLOAD_INVALID"
	ParentErrorNotFound      = "REVIEW_NOT_FOUND"
	ParentErrorVersion       = "REVIEW_VERSION_CONFLICT"
	ParentErrorInProgress    = "REVIEW_PARENT_ATTACHMENT_IN_PROGRESS"
	ParentErrorInputStale    = "REVIEW_PARENT_INPUT_STALE"
	ParentErrorFinalized     = "REVIEW_ALREADY_FINALIZED"
	ParentErrorNotRequired   = "REVIEW_PARENT_NOT_REQUIRED"
	ParentErrorArchiveUnsafe = "REVIEW_PARENT_ARCHIVE_UNSAFE"
	ParentErrorMismatch      = "REVIEW_PARENT_CONTENT_MISMATCH"
	ParentErrorStructure     = "REVIEW_PARENT_STRUCTURE_UNSUPPORTED"
	ParentErrorUnavailable   = "REVIEW_PARENT_VALIDATION_UNAVAILABLE"
)

type ParentAttachmentError struct {
	Code  string
	Cause error
}

func (value *ParentAttachmentError) Error() string {
	if value.Cause == nil {
		return value.Code
	}
	return fmt.Sprintf("%s: %v", value.Code, value.Cause)
}
func (value *ParentAttachmentError) Unwrap() error { return value.Cause }

func parentError(code string, cause error) error {
	return &ParentAttachmentError{Code: code, Cause: cause}
}

func parentStoreError(operation string, err error) error {
	return fmt.Errorf("libraryimport/arcade parent %s: %w", operation, err)
}

func ParentAttachmentErrorCode(err error) string {
	var value *ParentAttachmentError
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

type ParentAttachmentRequest struct {
	ValidationID         string `json:"validationId"`
	BaseSourceSnapshotID string `json:"baseSourceSnapshotId"`
	DependencyMachine    string `json:"dependencyMachine"`
	UploadFileID         string `json:"uploadFileId"`
}

type ParentAttachmentCreated struct {
	AttachmentID string `json:"attachmentId"`
	State        string `json:"state"`
	JobID        string `json:"jobId"`
	Version      int64  `json:"-"`
}

type parentAttachmentInput = application.ArcadeParentAttachmentInput

type parentAttachmentCandidate struct {
	attachmentID, itemID, draftID, baseSnapshotID    string
	machine, requiredBy, providerID, targetID, datID string
	uploadFileID, uploadSessionID, originalName      string
	blobID, blobSHA                                  string
	blobSize                                         int64
	contentPolicyDigest                              string
	depth                                            int
}

// Preconditions intentionally share one transaction and one stable error mapping.
func (service *Service) CreateArcadeParentAttachment(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	request ParentAttachmentRequest,
) (ParentAttachmentCreated, error) {
	if invalidParentAttachmentRequest(service, itemID, expectedVersion, request) {
		return ParentAttachmentCreated{}, parentError(ParentErrorInvalid, ErrInvalid)
	}
	var result ParentAttachmentCreated
	repository := librarypersistence.NewArcadeParentAttachments(service.database)
	err := repository.WithAdmission(ctx, func(scope application.ArcadeParentAttachmentAdmissionScope) error {
		setup := parentAttachmentSetup{
			service: service, ctx: ctx, scope: scope,
			itemID: itemID, expectedVersion: expectedVersion, request: request,
		}
		if err := setup.load(); err != nil {
			return err
		}
		var err error
		result, err = setup.persist()
		return err
	})
	if err != nil {
		err = fmt.Errorf("admit arcade parent attachment: %w", err)
		var known *ParentAttachmentError
		if errors.As(err, &known) {
			return ParentAttachmentCreated{}, err
		}
		if errors.Is(err, application.ErrArcadeParentAttachmentActive) {
			return ParentAttachmentCreated{}, parentError(ParentErrorInProgress, err)
		}
		if errors.Is(err, application.ErrVersionConflict) {
			return ParentAttachmentCreated{}, parentError(ParentErrorVersion, err)
		}
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	go service.runParentAttachment(context.WithoutCancel(ctx), result.JobID)
	return result, nil
}

func invalidParentAttachmentRequest(
	service *Service,
	itemID string,
	expectedVersion int64,
	request ParentAttachmentRequest,
) bool {
	return expectedVersion < 1 || itemID == "" || request.ValidationID == "" ||
		request.BaseSourceSnapshotID == "" || request.UploadFileID == "" ||
		!validArcadeMachine(request.DependencyMachine) || service.blobs == nil
}

type parentAttachmentSetup struct {
	service             *Service
	ctx                 context.Context
	scope               application.ArcadeParentAttachmentAdmissionScope
	itemID              string
	expectedVersion     int64
	request             ParentAttachmentRequest
	draftID             string
	targetID            string
	effectiveSnapshotID string
	platformID          string
	coreID              string
	providerID          string
	runtimeTargetID     string
	contentPolicy       contentcapability.Policy
	activeDATID         string
	hasActiveDAT        bool
	platformVersion     int64
	dependency          arcadeDraftDependency
	uploadSessionID     string
	originalName        string
	blobID              string
	blobSHA             string
	blobSize            int64
}

func (setup *parentAttachmentSetup) load() error {
	steps := []func() error{
		setup.loadDraft, setup.validateSelectedValidation,
		setup.loadUpload, setup.ensureNoActiveAttachment,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (setup *parentAttachmentSetup) loadDraft() error {
	draft, found, err := setup.scope.Read.Draft(setup.ctx, setup.itemID)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	if !found {
		return parentError(ParentErrorNotFound, application.ErrInvalid)
	}
	setup.draftID, setup.targetID, setup.effectiveSnapshotID = draft.DraftID, draft.TargetID, draft.EffectiveSnapshotID
	setup.platformID, setup.platformVersion = draft.PlatformID, draft.PlatformVersion
	setup.coreID, setup.providerID, setup.runtimeTargetID = draft.CoreID, draft.ProviderID, draft.RuntimeTargetID
	setup.contentPolicy = draft.ContentPolicy
	setup.activeDATID = draft.ActiveDATVersionID
	setup.hasActiveDAT = draft.HasActiveDAT
	if draft.ItemState != "REVIEW_PENDING" {
		return parentError(ParentErrorFinalized, ErrInvalid)
	}
	if draft.DraftVersion != setup.expectedVersion {
		return parentError(ParentErrorVersion, ErrInvalid)
	}
	if setup.platformID != "arcade" ||
		setup.effectiveSnapshotID != setup.request.BaseSourceSnapshotID || !setup.hasActiveDAT {
		return parentError(ParentErrorInputStale, ErrInvalid)
	}
	return nil
}

func (setup *parentAttachmentSetup) validateSelectedValidation() error {
	validation, found, err := setup.scope.Read.Validation(setup.ctx, setup.request.ValidationID, setup.itemID)
	if err != nil {
		return parentError(ParentErrorInputStale, err)
	}
	if !found {
		return parentError(ParentErrorInputStale, ErrInvalid)
	}
	if !setup.validationMatches(
		validation.TargetPlatformInstanceID, validation.SourceSnapshotID, validation.CoreID,
		validation.ProviderID, validation.TargetID, validation.DATVersionID, validation.HasDATVersion,
	) {
		return parentError(ParentErrorInputStale, ErrInvalid)
	}
	snapshot, err := setup.service.canonicalArcadeSnapshotWithQueryer(
		setup.ctx, setup.scope.Read, validation.DependencySnapshotJSON,
	)
	if err != nil {
		return parentError(ParentErrorInputStale, err)
	}
	dependency, found := attachmentDependency(snapshot, setup.request.DependencyMachine)
	if !found || dependency.Kind != "PARENT" ||
		(dependency.State != "MISSING" && dependency.State != "MISMATCH") {
		return parentError(ParentErrorNotRequired, ErrInvalid)
	}
	if dependency.RequiredBy == nil || dependency.Depth < 1 || dependency.Depth > 63 {
		return parentError(ParentErrorStructure, ErrInvalid)
	}
	setup.dependency = dependency
	return nil
}

func (setup *parentAttachmentSetup) validationMatches(
	targetID, snapshotID, coreID, providerID, runtimeTargetID string,
	datID string, hasDAT bool,
) bool {
	return targetID == setup.targetID &&
		coreID == setup.coreID && providerID == setup.providerID && runtimeTargetID == setup.runtimeTargetID &&
		snapshotID == setup.effectiveSnapshotID &&
		hasDAT && datID == setup.activeDATID
}

func (setup *parentAttachmentSetup) loadUpload() error {
	upload, found, err := setup.scope.Read.Upload(setup.ctx, setup.request.UploadFileID)
	if err != nil {
		return parentError(ParentErrorInvalid, err)
	}
	if !found || upload.SessionState != "COMPLETE" || upload.FileState != "COMPLETE" ||
		upload.WholeSessionConsumed || !strings.EqualFold(filepath.Ext(upload.RelativePath), ".zip") {
		return parentError(ParentErrorInvalid, ErrInvalid)
	}
	setup.uploadSessionID, setup.originalName = upload.UploadSessionID, upload.RelativePath
	setup.blobID, setup.blobSHA, setup.blobSize = upload.BlobID, upload.BlobSHA, upload.BlobSize
	info, err := os.Stat(setup.service.blobs.Path(setup.blobSHA))
	if err != nil || !info.Mode().IsRegular() || info.Size() != setup.blobSize {
		return parentError(ParentErrorInvalid, err)
	}
	return nil
}

func (setup *parentAttachmentSetup) ensureNoActiveAttachment() error {
	active, err := setup.scope.Read.HasActive(setup.ctx, setup.itemID)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	if active {
		return parentError(ParentErrorInProgress, ErrInvalid)
	}
	return nil
}

func (setup *parentAttachmentSetup) persist() (ParentAttachmentCreated, error) {
	attachmentID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	input := setup.input(attachmentID.String())
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	now := setup.service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(strings.Join([]string{
		setup.itemID, setup.effectiveSnapshotID, setup.dependency.Machine,
		setup.blobSHA, setup.request.ValidationID,
	}, "\x00")))
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": setup.dependency.Machine,
		"originalFilename": filepath.Base(setup.originalName), "state": "QUEUED",
	})
	err := setup.scope.Write.Create(setup.ctx, application.ArcadeParentAttachmentWrite{
		Input: input, InputJSON: string(inputJSON),
		InputDigest: hex.EncodeToString(inputDigest[:]), DedupeKey: hex.EncodeToString(dedupe[:]),
		AttachmentID: attachmentID.String(), JobID: jobID.String(), ItemID: setup.itemID,
		DraftID: setup.draftID, BaseSourceSnapshotID: setup.effectiveSnapshotID,
		DependencyMachine: setup.dependency.Machine, RequiredByMachine: *setup.dependency.RequiredBy,
		Depth: setup.dependency.Depth, ProviderID: setup.providerID, TargetID: setup.runtimeTargetID,
		DATVersionID: setup.activeDATID, UploadID: setup.request.UploadFileID,
		OriginalFilename: filepath.Base(setup.originalName), ExpectedDraftVersion: setup.expectedVersion,
		NowMS: now, Actor: reviewActor(setup.ctx), EvidenceJSON: evidence,
	})
	if err != nil {
		if errors.Is(err, application.ErrArcadeParentAttachmentActive) {
			return ParentAttachmentCreated{}, parentError(ParentErrorInProgress, err)
		}
		if errors.Is(err, application.ErrVersionConflict) {
			return ParentAttachmentCreated{}, parentError(ParentErrorVersion, err)
		}
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	return ParentAttachmentCreated{
		AttachmentID: attachmentID.String(), State: "QUEUED",
		JobID: jobID.String(), Version: setup.expectedVersion + 1,
	}, nil
}

func (setup *parentAttachmentSetup) input(attachmentID string) parentAttachmentInput {
	return parentAttachmentInput{
		SchemaVersion: 1, AttachmentID: attachmentID, ImportItemID: setup.itemID,
		ReviewDraftID: setup.draftID, BaseSourceSnapshotID: setup.effectiveSnapshotID,
		DependencyMachine: setup.dependency.Machine, ProviderID: setup.providerID,
		TargetID: setup.runtimeTargetID, ContentPolicyDigest: setup.contentPolicy.DigestFor("SINGLE_FILE"),
		DATVersionID: setup.activeDATID, UploadFileID: setup.request.UploadFileID,
	}
}

func validArcadeMachine(value string) bool {
	return validField(value, 255, false) && value != "" && !strings.ContainsAny(value, "/\\")
}

func attachmentDependency(snapshot arcadeDraftSnapshot, machine string) (arcadeDraftDependency, bool) {
	for _, dependency := range snapshot.Dependencies {
		if dependency.Machine == machine {
			return dependency, true
		}
	}
	return arcadeDraftDependency{}, false
}

func (service *Service) canonicalArcadeSnapshotWithQueryer(
	ctx context.Context,
	reader application.ArcadeRelationReader,
	raw string,
) (arcadeDraftSnapshot, error) {
	snapshot, err := application.CanonicalArcadeSnapshot(ctx, reader, raw)
	if err != nil {
		return arcadeDraftSnapshot{}, fmt.Errorf("read canonical arcade snapshot: %w", err)
	}
	return snapshot, nil
}

func (service *Service) ResumeParentAttachmentJobs(ctx context.Context) {
	jobIDs, err := librarypersistence.NewReviewArcadeParentJobs(service.database).Queued(ctx)
	if err != nil {
		return
	}
	workerContext := context.WithoutCancel(ctx)
	for _, jobID := range jobIDs {
		go service.runParentAttachment(workerContext, jobID)
	}
}
