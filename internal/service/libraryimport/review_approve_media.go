package libraryimport

import (
	"fmt"
	model "retrom/internal/model/libraryimport"
)

func (run *reviewApprovalRun) prepareOrigin() error {
	decision := run.request.Decision
	run.origin = model.ApprovalOrigin{Kind: "IMPORT_REVIEW", RefID: run.request.ItemID}
	if decision.SourceKind != "" {
		run.origin = model.ApprovalOrigin{
			Kind: decision.SourceKind, RefID: decision.SourceRefID,
			Assets: decision.ExternalAssets,
		}
		return nil
	}
	origin, found, err := run.scope.Reader.Origin(run.ctx, run.request.ItemID)
	if err != nil {
		return fmt.Errorf("read approval origin: %w", err)
	}
	if found {
		run.origin = origin
	}
	return nil
}

func (run *reviewApprovalRun) prepareAssets() error {
	for _, selection := range []struct {
		id       *string
		kind     string
		uploaded bool
	}{
		{run.head.CoverID, "COVER", false},
		{run.head.UploadedCoverID, "COVER", true},
		{run.head.BackgroundID, "BACKGROUND", false},
	} {
		if selection.id != nil {
			if err := run.appendSelectedAsset(*selection.id, selection.kind, 0,
				selection.uploaded); err != nil {
				return err
			}
		}
	}
	ids, err := run.scope.Media.Screenshots(run.ctx, run.request.ItemID)
	if err != nil {
		return fmt.Errorf("read approved screenshots: %w", err)
	}
	run.screenshotIDs = ids
	for ordinal, id := range ids {
		if err := run.appendSelectedAsset(id, "SCREENSHOT", ordinal, false); err != nil {
			return err
		}
	}
	return run.appendExternalAssets()
}

func (run *reviewApprovalRun) appendSelectedAsset(id, kind string, ordinal int, uploaded bool) error {
	var asset model.ApprovalExternalAsset
	var found bool
	var err error
	if uploaded {
		asset, found, err = run.scope.Media.UploadedCover(run.ctx, run.request.ItemID, id)
	} else {
		asset, found, err = run.scope.Media.Candidate(run.ctx, run.request.ItemID, id)
	}
	if err != nil {
		return fmt.Errorf("read selected approval asset: %w", err)
	}
	if !found {
		return model.ErrInvalid
	}
	asset.Kind = kind
	run.assets = append(run.assets, model.ApprovalAsset{ApprovalExternalAsset: asset, Ordinal: ordinal})
	return nil
}

func (run *reviewApprovalRun) appendExternalAssets() error {
	selected := make([]model.ApprovalExternalAsset, 0, len(run.origin.Assets))
	for _, asset := range run.origin.Assets {
		if run.request.Decision.SourceKind == "" && asset.Kind == "COVER" &&
			(run.head.CoverID != nil || run.head.UploadedCoverID != nil) {
			continue
		}
		selected = append(selected, asset)
	}
	if !ValidApprovalExternalAssets(selected) {
		return model.ErrInvalid
	}
	for _, asset := range selected {
		found, err := run.scope.Media.BlobExists(run.ctx, asset.BlobID)
		if err != nil {
			return fmt.Errorf("read approval source asset: %w", err)
		}
		if !found {
			return model.ErrInvalid
		}
		run.assets = append(run.assets, model.ApprovalAsset{ApprovalExternalAsset: asset})
	}
	return nil
}
