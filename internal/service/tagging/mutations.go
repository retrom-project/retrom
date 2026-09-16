package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

func (service *Service) Create(ctx context.Context, actorUserID, rawName string) (model.AdminItem, error) {
	if !model.ValidID(actorUserID) {
		return model.AdminItem{}, model.ErrInvalid
	}
	name, key, search, err := model.NormalizeName(rawName)
	if err != nil {
		return model.AdminItem{}, fmt.Errorf("normalize name: %w", err)
	}
	tagID, err := service.newID()
	if err != nil {
		return model.AdminItem{}, repositoryError("create id", err)
	}
	auditID, err := service.newID()
	if err != nil {
		return model.AdminItem{}, repositoryError("create audit id", err)
	}
	result, err := service.commands.CommitCreate(ctx, model.CreateCommand{
		TagID: tagID, AuditID: auditID, ActorUserID: actorUserID,
		Name: name, NameKey: key, SearchText: search,
		NowMS: service.now().UnixMilli(),
	})
	return result, repositoryError("create", err)
}

func (service *Service) Rename(
	ctx context.Context,
	actorUserID, tagID, rawName string,
	expectedVersion int64,
) (model.AdminItem, error) {
	if !model.ValidID(actorUserID) || !model.ValidID(tagID) || expectedVersion < 1 {
		return model.AdminItem{}, model.ErrInvalid
	}
	name, key, search, err := model.NormalizeName(rawName)
	if err != nil {
		return model.AdminItem{}, fmt.Errorf("normalize name: %w", err)
	}
	auditID, err := service.newID()
	if err != nil {
		return model.AdminItem{}, repositoryError("rename audit id", err)
	}
	result, err := service.commands.CommitRename(ctx, model.RenameCommand{
		TagID: tagID, AuditID: auditID, ActorUserID: actorUserID,
		Name: name, NameKey: key, SearchText: search,
		ExpectedVersion: expectedVersion, NowMS: service.now().UnixMilli(),
	})
	return result, repositoryError("rename", err)
}

func (service *Service) Delete(
	ctx context.Context,
	actorUserID, tagID, confirmName string,
	expectedVersion int64,
) (model.AdminItem, model.DeleteImpact, error) {
	if !model.ValidID(actorUserID) || !model.ValidID(tagID) || expectedVersion < 1 {
		return model.AdminItem{}, model.DeleteImpact{}, model.ErrInvalid
	}
	auditID, err := service.newID()
	if err != nil {
		return model.AdminItem{}, model.DeleteImpact{}, repositoryError("delete audit id", err)
	}
	result, impact, err := service.commands.CommitDelete(ctx, model.DeleteCommand{
		TagID: tagID, AuditID: auditID, ActorUserID: actorUserID,
		ConfirmName:     confirmName,
		ExpectedVersion: expectedVersion, NowMS: service.now().UnixMilli(),
	})
	return result, impact, repositoryError("delete", err)
}
