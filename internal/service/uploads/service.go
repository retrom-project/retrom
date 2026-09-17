package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"retrom/internal/adapter/files/uploadfiles"

	"github.com/google/uuid"
)

type Service struct {
	repository      Repository
	blobs           BlobWriter
	dataDir         string
	now             func() time.Time
	source          *uploadfiles.Store
	mutex           sync.Mutex
	group           sync.WaitGroup
	closed, started bool
	active          map[string]context.CancelCauseFunc
}

func New(repository Repository, blobs BlobWriter, dataDir string, now func() time.Time) *Service {
	return &Service{
		repository: repository, blobs: blobs, dataDir: dataDir, now: now,
		source: uploadfiles.New(dataDir), active: make(map[string]context.CancelCauseFunc),
	}
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (Session, error) {
	if request.Purpose == "" {
		request.Purpose = "GENERAL"
	}
	total, err := validateCreateRequest(request)
	if err != nil {
		return Session{}, err
	}
	manifest, err := json.Marshal(request)
	if err != nil {
		return Session{}, fmt.Errorf("encode upload manifest: %w", err)
	}
	digest := sha256.Sum256(manifest)
	id, err := uuid.NewV7()
	if err != nil {
		return Session{}, fmt.Errorf("generate upload ID: %w", err)
	}
	now := service.now().UnixMilli()
	session := Session{
		ID: id.String(), State: "CREATED", Purpose: request.Purpose, SourceType: request.SourceType, TotalBytes: total,
		Version: 1, ExpiresAtMS: now + int64(
			24*time.Hour/time.Millisecond,
		), ChunkSizeBytes: PartSize, Files: make(
			[]File,
			0,
			len(
				request.Files,
			),
		),
	}
	for _, declaration := range request.Files {
		id, err := uuid.NewV7()
		if err != nil {
			return Session{}, fmt.Errorf("generate upload file ID: %w", err)
		}
		session.Files = append(session.Files, File{
			ID: id.String(), ClientFileID: declaration.ClientFileID, RelativePath: declaration.RelativePath,
			SizeBytes: declaration.SizeBytes, State: "PENDING", Parts: []int{},
		})
	}
	err = service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		return scope.Sessions.Create(
			ctx,
			Registration{
				Session: session,
				ManifestDigest: hex.EncodeToString(
					digest[:],
				),
				AtMS: now,
			},
		)
	})
	if err != nil {
		return Session{}, fmt.Errorf("create upload: %w", err)
	}
	return session, nil
}

func (service *Service) Get(ctx context.Context, id string) (Session, error) {
	session, err := service.repository.Snapshot(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("read upload: %w", err)
	}
	session.ChunkSizeBytes = PartSize
	return session, nil
}

func (service *Service) PutPart(
	ctx context.Context,
	uploadID, fileID string,
	partNo int,
	contentRange, contentDigest string,
	body io.Reader,
) error {
	span, err := parseRange(contentRange)
	if err != nil || partNo < 0 || span.start%PartSize != 0 || span.start/PartSize != int64(
		partNo,
	) || span.end != min(
		span.total-1,
		span.start+PartSize-1,
	) {
		return ErrInvalid
	}
	expected, err := parseDigest(contentDigest)
	if err != nil {
		return err
	}
	key := FileKey{UploadID: uploadID, FileID: fileID}
	if err := service.validateReceivingPart(ctx, key, span.total, partNo); err != nil {
		return err
	}
	written, err := service.stageUploadPart(uploadID, fileID, partNo, span, expected, body)
	if err != nil {
		return err
	}
	now := service.now().UnixMilli()
	part := PartRecord{FileID: fileID, AtMS: now, Part: Part{
		Number: partNo, Offset: span.start, Size: written, SHA256: expected,
		Path: filepath.ToSlash(filepath.Join(uploadID, fileID, strconv.Itoa(partNo)+"-"+expected)),
	}}
	err = service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		target, err := scope.Files.Target(ctx, key)
		if err != nil {
			return fmt.Errorf("recheck upload part target: %w", err)
		}
		if err := validatePartTarget(target, span.total, now); err != nil {
			return err
		}
		if target.SessionState == "FAILED" {
			if err := repairAllowed(ctx, scope, key, partNo); err != nil {
				return err
			}
		}
		return recordPart(ctx, scope, target, part)
	})
	if err != nil {
		return fmt.Errorf("record upload part: %w", err)
	}
	return nil
}

func validatePartTarget(target PartTarget, total, now int64) error {
	if target.DeclaredSize != total || target.FileState == "COMPLETE" || target.FileState == "FINALIZING" ||
		now >= target.ExpiresAtMS {
		return ErrInvalid
	}
	if target.SessionState == "CREATED" || target.SessionState == "UPLOADING" {
		return nil
	}
	if target.SessionState == "FAILED" && target.LastErrorCode != nil &&
		(*target.LastErrorCode == "UPLOAD_PART_MISSING" || *target.LastErrorCode == "UPLOAD_PART_CORRUPT") {
		return nil
	}
	return ErrInvalid
}

func recordPart(ctx context.Context, scope WriteScope, target PartTarget, part PartRecord) error {
	inserted, err := scope.Parts.Put(ctx, part)
	if err != nil {
		return fmt.Errorf("write upload part: %w", err)
	}
	if !inserted {
		existing, found, err := scope.Parts.Get(ctx, part.FileID, part.Part.Number)
		if err != nil {
			return fmt.Errorf("read upload part replay: %w", err)
		}
		if !found || existing.Part.SHA256 != part.Part.SHA256 ||
			existing.Part.Offset != part.Part.Offset || existing.Part.Size != part.Part.Size {
			return ErrInvalid
		}
		return nil
	}
	if err := scope.Files.AddReceived(
		ctx,
		FileProgress{
			FileID: part.FileID,
			Bytes:  part.Part.Size,
			AtMS:   part.AtMS,
		},
	); err != nil {
		return fmt.Errorf("advance upload file: %w", err)
	}
	if err := scope.Sessions.Advance(
		ctx,
		SessionProgress{
			ID:              target.UploadID,
			State:           "UPLOADING",
			ExpectedVersion: target.SessionVersion,
			AtMS:            part.AtMS,
		},
	); err != nil {
		return fmt.Errorf("advance upload session: %w", err)
	}
	return nil
}

func repairAllowed(ctx context.Context, scope WriteScope, key FileKey, number int) error {
	allowed, err := scope.Finalize.Repair(ctx, key, number)
	if err != nil {
		return fmt.Errorf("read upload repair authorization: %w", err)
	}
	if !allowed {
		return ErrInvalid
	}
	return nil
}

func (service *Service) validateReceivingPart(ctx context.Context, key FileKey, total int64, number int) error {
	target, err := service.repository.Target(ctx, key)
	if err != nil {
		return finalizationError("read upload part target", err)
	}
	if err := validatePartTarget(target, total, service.now().UnixMilli()); err != nil {
		return err
	}
	if target.SessionState != "FAILED" {
		return nil
	}
	err = service.repository.CommitWrite(ctx, func(scope WriteScope) error { return repairAllowed(ctx, scope, key, number) })
	return finalizationError("validate repair target", err)
}
