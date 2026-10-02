package libraryimport

import "fmt"

func (run *reviewApprovalRun) prepareOrigin() error {
	decision := run.request.Decision
	run.origin = ApprovalOrigin{Kind: "IMPORT_REVIEW"}
	if decision.SourceKind != "" {
		run.origin = ApprovalOrigin{
			Kind:   decision.SourceKind,
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
		{run.head.UploadedVideoID, "VIDEO", true},
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
	if err := run.appendRuntimeScreenshot(len(ids)); err != nil {
		return err
	}
	return run.appendExternalAssets()
}

func (run *reviewApprovalRun) appendRuntimeScreenshot(ordinal int) error {
	if run.head.ScreenshotID == nil {
		return nil
	}
	asset, found, err := run.scope.Media.RuntimeScreenshot(run.ctx, run.request.ItemID, *run.head.ScreenshotID)
	if err != nil {
		return fmt.Errorf("read captured approval screenshot: %w", err)
	}
	if !found {
		return ErrInvalid
	}
	asset.Kind = "SCREENSHOT"
	run.assets = append(run.assets, ApprovalAsset{ApprovalExternalAsset: asset, Ordinal: ordinal})
	return nil
}

func (run *reviewApprovalRun) appendSelectedAsset(id, kind string, ordinal int, uploaded bool) error {
	var asset ApprovalExternalAsset
	var found bool
	var err error
	if uploaded {
		asset, found, err = run.scope.Media.UploadedAsset(run.ctx, run.request.ItemID, id, kind)
	} else {
		asset, found, err = run.scope.Media.Candidate(run.ctx, run.request.ItemID, id)
	}
	if err != nil {
		return fmt.Errorf("read selected approval asset: %w", err)
	}
	if !found {
		return ErrInvalid
	}
	asset.Kind = kind
	run.assets = append(run.assets, ApprovalAsset{ApprovalExternalAsset: asset, Ordinal: ordinal})
	return nil
}

func (run *reviewApprovalRun) appendExternalAssets() error {
	selected := make([]ApprovalExternalAsset, 0, len(run.origin.Assets))
	for _, asset := range run.origin.Assets {
		if asset.Kind == "VIDEO" && run.request.Decision.SourceKind == "" && run.head.UploadedVideoID != nil {
			continue
		}
		if run.request.Decision.SourceKind == "" && asset.Kind == "COVER" &&
			(run.head.CoverID != nil || run.head.UploadedCoverID != nil) {
			continue
		}
		selected = append(selected, asset)
	}
	if !ValidApprovalExternalAssets(selected) {
		return ErrInvalid
	}
	for _, asset := range selected {
		run.assets = append(run.assets, ApprovalAsset{ApprovalExternalAsset: asset})
	}
	return nil
}
