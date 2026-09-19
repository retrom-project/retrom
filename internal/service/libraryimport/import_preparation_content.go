package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentcapability"
	model "retrom/internal/model/libraryimport"
)

func (service *ImportPreparation) prepareContent(
	ctx context.Context,
	plan *model.PreparedImport,
) error {
	var err error
	capabilities := contentcapability.Resolve(
		plan.Target.PlatformID, true, service.options.MultiDiscEnabled, plan.Target.Policy,
	)
	switch plan.ContentMode {
	case contentcapability.ModeMultiDisc:
		plan.Dispositions, plan.Groups, err = service.PrepareMultiDiscFiles(plan.Files, *capabilities.MultiDisc)
	case contentcapability.ModeRPGMakerProject:
		if plan.Target.PlatformID != "rpgmaker" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareRPGMakerProject(
			ctx, plan.SourceType, plan.Files, plan.Target.DefaultCoreID,
		)
	case contentcapability.ModeONSProject, contentcapability.ModeKiriKiriProject, contentcapability.ModeNXEngineProject,
		contentcapability.ModeButterscotchProject, contentcapability.ModeTyranoScriptProject,
		contentcapability.ModeScummVMProject:
		return service.prepareEngineProject(ctx, plan)
	case contentcapability.ModeStandard:
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareImportFiles(
			ctx, plan.Target.PlatformID, plan.SourceType, plan.Files, plan.DATVersionID,
		)
	default:
		return model.ErrInvalid
	}
	return err
}

func (service *ImportPreparation) prepareEngineProject(ctx context.Context, plan *model.PreparedImport) error {
	var err error
	switch plan.ContentMode {
	case contentcapability.ModeONSProject:
		if plan.Target.PlatformID != "ons" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareONSProject(
			ctx, plan.SourceType, plan.Files,
		)
	case contentcapability.ModeKiriKiriProject:
		if plan.Target.PlatformID != "kirikiri" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareKiriKiriProject(
			ctx, plan.SourceType, plan.Files,
		)
	case contentcapability.ModeNXEngineProject:
		if plan.Target.PlatformID != "cavestory" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareNXEngineProject(ctx, plan.SourceType, plan.Files)
	case contentcapability.ModeButterscotchProject:
		if plan.Target.PlatformID != "butterscotch" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareButterscotchProject(
			ctx, plan.SourceType, plan.Files,
		)
	case contentcapability.ModeTyranoScriptProject:
		if plan.Target.PlatformID != "tyranoscript" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareTyranoScriptProject(
			ctx, plan.SourceType, plan.Files,
		)
	case contentcapability.ModeScummVMProject:
		if plan.Target.PlatformID != "scummvm" {
			return model.ErrInvalid
		}
		plan.Dispositions, plan.Groups, plan.Archives, err = service.PrepareScummVMProject(ctx, plan.SourceType, plan.Files)
	default:
		return model.ErrInvalid
	}
	return err
}
