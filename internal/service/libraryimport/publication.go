package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/filestore"
)

func (service *ReviewApprovals) preparePublication(ctx context.Context,
	request ReviewApprovalRequest,
) (PublicationState, error) {
	var state PublicationState
	err := service.repository.WithApproval(ctx, func(scope ReviewApprovalScope) error {
		var err error
		state, err = scope.Publications.ReadPublication(ctx, request.ItemID)
		if err != nil {
			return fmt.Errorf("prepare publication: %w", err)
		}
		if !state.Found {
			return ErrInvalid
		}
		if state.State == "PUBLISHED" || state.Intent != nil {
			return nil
		}
		run := reviewApprovalRun{ctx: ctx, service: service, scope: scope, request: request}
		for _, step := range []func() error{run.load, run.prepare, run.claimDuplicates} {
			if err := step(); err != nil {
				return err
			}
		}
		if request.Bulk != nil {
			if err := scope.Bulk.CheckRequest(ctx, request, run.now); err != nil {
				return fmt.Errorf("prepare publication: %w", err)
			}
		}
		actor, _ := authn.PrincipalFromContext(ctx)
		intent := Publication{
			IdentityDigest: run.identityDigest, Request: request, ActorID: actor.UserID,
			ProfileID: actor.ProfileID, Head: run.head, Metadata: run.metadata, Origin: run.origin,
			Assets: run.assets, ScreenshotIDs: run.screenshotIDs, GameID: run.gameID, VariantID: run.variantID,
			ScreenshotOverride: run.screenshotOverride, RuntimeDependencyJSON: run.runtimeDependencyJSON,
			RPGProfile: run.rpgProfile, RPGDependencies: run.rpgDependencies,
		}
		values, err := scope.Publications.PublicationFiles(ctx, run.head)
		if err != nil {
			return fmt.Errorf("prepare publication: %w", err)
		}
		intent.Files, err = publicationFiles(values, request.ItemID, run.head.SourceSnapshotID)
		if err != nil {
			return err
		}
		if err := scope.Publications.BeginPublication(ctx, intent, run.now); err != nil {
			return fmt.Errorf("prepare publication: %w", err)
		}
		state.Intent, state.GameID, state.State = &intent, intent.GameID, "PUBLISHING"
		return nil
	})
	if err != nil {
		return PublicationState{}, fmt.Errorf("prepare game publication: %w", err)
	}
	return state, nil
}

func (service *ReviewApprovals) publishDirectory(ctx context.Context, intent Publication) ([]ApprovalAsset, error) {
	if service.files == nil {
		return nil, ErrInvalid
	}
	moved, err := service.files.DirectoryPublished(intent.Request.ItemID, intent.GameID)
	if err != nil {
		return nil, fmt.Errorf("publish directory: %w", err)
	}
	if !moved {
		if err := service.copyPublicationFiles(ctx, intent.Files); err != nil {
			return nil, err
		}
	}
	assets := append([]ApprovalAsset(nil), intent.Assets...)
	for i := range assets {
		asset := &assets[i]
		file, err := filestore.ParseRecord(asset.FileRecord)
		if err != nil {
			return nil, fmt.Errorf("publish directory: %w", err)
		}
		name := strings.ToLower(asset.Kind) + "-" + asset.ID + publicationExtension(asset.MediaType)
		if !moved {
			prepared, err := service.files.CopyTo(ctx, asset.FileRecord,
				filestore.ItemDirectory(intent.Request.ItemID)+"/payload/media/"+asset.ID, name)
			if err != nil {
				return nil, fmt.Errorf("publish directory: %w", err)
			}
			file, err = filestore.ParseRecord(prepared.Record)
			if err != nil {
				return nil, fmt.Errorf("publish directory: %w", err)
			}
		}
		file.Path = filestore.GameDirectory(intent.GameID) + "/media/" + asset.ID + "/" + name
		asset.FileRecord, err = file.Encode()
		if err != nil {
			return nil, fmt.Errorf("publish directory: %w", err)
		}
	}
	if err := service.files.PublishDirectory(intent.Request.ItemID, intent.GameID); err != nil {
		return nil, fmt.Errorf("publish directory: %w", err)
	}
	return assets, nil
}

func (service *ReviewApprovals) completePublication(ctx context.Context, intent Publication,
	assets []ApprovalAsset,
) (ReviewApproved, error) {
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: intent.ActorID, ProfileID: intent.ProfileID, Role: "ADMIN"})
	result := ReviewApproved{GameID: intent.GameID, Status: "PUBLISHED"}
	err := service.repository.WithApproval(ctx, func(scope ReviewApprovalScope) error {
		state, err := scope.Publications.ReadPublication(ctx, intent.Request.ItemID)
		if err != nil {
			return fmt.Errorf("complete publication: %w", err)
		}
		if state.State == "PUBLISHED" && state.GameID == intent.GameID {
			return nil
		}
		if state.State != "PUBLISHING" || state.Intent == nil || state.GameID != intent.GameID {
			return ErrInvalid
		}
		run := reviewApprovalRun{
			ctx: ctx, service: service, scope: scope, request: intent.Request, head: intent.Head,
			metadata: intent.Metadata, origin: intent.Origin, assets: assets, screenshotIDs: intent.ScreenshotIDs,
			gameID: intent.GameID, variantID: intent.VariantID, now: service.now().UnixMilli(),
			screenshotOverride: intent.ScreenshotOverride, runtimeDependencyJSON: intent.RuntimeDependencyJSON,
			rpgProfile: intent.RPGProfile, rpgDependencies: intent.RPGDependencies,
		}
		run.head.RuntimeFiles = stagePublicationRuntimeFiles(intent.Head.RuntimeFiles, intent.Files)
		run.head.Progress, run.head.ParentVersion, err = scope.Publications.PublicationProgress(ctx, run.head.ImportID)
		if err != nil {
			return fmt.Errorf("complete publication: %w", err)
		}
		if err := run.projectAggregate(); err != nil {
			return err
		}
		if err := scope.Publications.PreparePublicationRecords(ctx, intent); err != nil {
			return fmt.Errorf("complete publication: %w", err)
		}
		return run.publish()
	})
	if err != nil {
		return ReviewApproved{}, fmt.Errorf("complete game publication: %w", err)
	}
	return result, nil
}

func publicationExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ".bin"
	}
}

func (service *ReviewApprovals) Recover(ctx context.Context) error {
	requests, err := service.repository.PendingPublications(ctx)
	if err != nil {
		return fmt.Errorf("recover: %w", err)
	}
	var failures []error
	for _, request := range requests {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		if _, err := service.Approve(ctx, request); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
