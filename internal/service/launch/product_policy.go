package launch

import (
	"reflect"
	"slices"

	"retrom/internal/capability/content/multidisc"
	model "retrom/internal/model/launch"
)

func validProductRequest(command model.ProductCreateCommand) bool {
	request := command.Request
	if command.ProfileID == "" || request.GameID == "" || !ValidProductReturnTo(
		request.ReturnTo,
		request.GameID,
		request.SaveStateID,
	) {
		return false
	}
	if request.SaveStateID != nil && request.DOSEntry != nil {
		return false
	}
	return command.Key == "" || command.ActorID != "" && len(command.Digest) == 64
}

func validateProductSelection(command model.ProductCreateCommand, snapshot model.ProductSnapshot) error {
	if command.Request.SaveStateID != nil {
		return validateProductSavedSelection(snapshot)
	}
	if !snapshot.Found {
		return model.ErrBlocked
	}
	return nil
}

func validateProductSavedSelection(snapshot model.ProductSnapshot) error {
	if snapshot.Save == nil {
		return model.ErrBlocked
	}
	if snapshot.Found && snapshot.Source.VariantStatus == "READY" && slices.Contains(
		snapshot.Source.ReadFormats,
		snapshot.Save.Format,
	) {
		return nil
	}
	if !snapshot.SaveReadable {
		return model.ErrSaveIncompatible
	}
	return model.ErrBlocked
}

type productDOSEntryChoice struct{ Path *string }

func selectedProductDOS(command model.ProductCreateCommand, snapshot model.ProductSnapshot) (
	productDOSEntryChoice,
	error,
) {
	entry := command.Request.DOSEntry
	if snapshot.Save != nil && snapshot.Save.DOSEntry != nil {
		entry = snapshot.Save.DOSEntry
	}
	if entry == nil {
		return productDOSEntryChoice{}, nil
	}
	if !snapshot.DOS.Found {
		return productDOSEntryChoice{}, model.ErrDOSEntryMissing
	}
	if !snapshot.DOS.Safe {
		return productDOSEntryChoice{}, model.ErrDOSEntryUnsafe
	}
	return productDOSEntryChoice{Path: entry}, nil
}

func productInitialDisc(snapshot model.ProductSnapshot, discCount int) (int64, error) {
	var saved *int64
	if snapshot.Save != nil {
		saved = snapshot.Save.DiscIndex
	}
	if snapshot.Source.ContentKind != multidisc.ContentKind {
		if saved != nil {
			return 0, model.ErrBlocked
		}
		return 0, nil
	}
	if snapshot.Save == nil {
		if saved != nil {
			return 0, model.ErrBlocked
		}
		return 0, nil
	}
	if saved == nil || *saved < 0 || *saved >= int64(discCount) {
		return 0, model.ErrBlocked
	}
	return *saved, nil
}

func sameProductInputs(before, after model.ProductSnapshot, validation bool) bool {
	before.SaveReadable, after.SaveReadable = false, false
	if !before.Found || !after.Found {
		return false
	}
	if validation {
		before.Source.VariantID, after.Source.VariantID = "", ""
		before.Source.VariantStatus, after.Source.VariantStatus = "", ""
		before.Source.DependencySnapshot, after.Source.DependencySnapshot = "", ""
		before.Source.CompatibilityCode, after.Source.CompatibilityCode = "", ""
		before.Source.DATVersionID, after.Source.DATVersionID = nil, nil
		before.VariantFiles, after.VariantFiles = nil, nil
		before.BIOS, after.BIOS = model.ProductBIOSFacts{}, model.ProductBIOSFacts{}
	} else {
		before.Source.GameVersion, after.Source.GameVersion = 0, 0
	}
	return reflect.DeepEqual(before, after)
}
