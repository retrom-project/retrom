package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
	"retrom/internal/temporary"

	"github.com/google/uuid"
)

type (
	Service struct {
		Storage           *storage.Store
		Repository        *persistence.Repository
		Runtime           *runtimeclient.Client
		Redis             *temporary.Redis
		Origin            string
		IsolationTemplate string
		Now               func() time.Time
	}
	Blob struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		Key         string `json:"key"`
		LogicalPath string `json:"logicalPath"`
		SHA256      string `json:"sha256"`
		SizeBytes   int64  `json:"sizeBytes"`
		MediaType   string `json:"mediaType"`
	}
	Context struct {
		Run         model.Run                 `json:"run"`
		UserID      string                    `json:"userId"`
		ContentHash string                    `json:"contentHash"`
		Files       []Blob                    `json:"files"`
		Indexes     map[string][]Blob         `json:"indexes"`
		Checkpoint  *runtimeclient.Checkpoint `json:"checkpoint"`
	}
)

const lifetime = 2 * time.Hour

func (s *Service) Create(ctx context.Context, p model.Principal, input model.RunInput) (model.Run, error) {
	status := "published"
	if input.Purpose == "review" {
		if err := p.Admin(); err != nil {
			return model.Run{}, wrap(err)
		}
		status = "pending_review"
	} else if input.Purpose != "play" {
		return model.Run{}, model.ErrInvalid
	}
	detail, err := s.Repository.GameDetail(ctx, p.User.ID, input.GameID, status)
	if err != nil {
		return model.Run{}, wrap(err)
	}
	directory, err := s.Repository.Directory(ctx, detail.Game.PlatformInstanceID)
	if err != nil {
		return model.Run{}, wrap(err)
	}
	if !directory.Enabled {
		return model.Run{}, model.ErrNotFound
	}
	prepared, save, err := s.prepare(ctx, p, input, detail, directory)
	if err != nil {
		return model.Run{}, wrap(err)
	}
	context := Context{
		UserID:      p.User.ID,
		ContentHash: detail.Game.ContentHash,
		Files: make([]Blob,
			0),
		Indexes:    make(map[string][]Blob),
		Checkpoint: prepared.Checkpoint,
	}
	context.Run = model.Run{
		ID:              uuid.NewString(),
		GameID:          input.GameID,
		Purpose:         input.Purpose,
		CoreID:          prepared.CoreID,
		ProviderID:      prepared.ProviderID,
		TargetID:        prepared.TargetID,
		CoreFingerprint: prepared.CoreFingerprint,
		ROMHash:         prepared.ROMHash,
		ExpiresAtMs:     s.Now().Add(lifetime).UnixMilli(),
	}
	if err = s.envelope(ctx, &context, detail, prepared, save); err != nil {
		return model.Run{}, err
	}
	if err = s.store(ctx, context); err != nil {
		return model.Run{}, err
	}
	return context.Run, nil
}

func (s *Service) Get(ctx context.Context, p model.Principal, id string) (Context, error) {
	raw, err := s.Redis.HashValue(ctx, "run:"+id, "context", lifetime)
	if err != nil {
		return Context{}, wrap(err)
	}
	var result Context
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		return result, fmt.Errorf("decode run context: %w", err)
	}
	if result.UserID != p.User.ID {
		return Context{}, model.ErrNotFound
	}
	status := "published"
	if result.Run.Purpose == "review" {
		if err = p.Admin(); err != nil {
			return Context{}, wrap(err)
		}
		status = "pending_review"
	}
	err = s.Repository.GameAccessible(ctx, result.Run.GameID, status)
	if err != nil {
		return Context{}, wrap(err)
	}
	result.Run.ExpiresAtMs = s.Now().Add(lifetime).UnixMilli()
	return result, nil
}

func (s *Service) store(ctx context.Context, value Context) error {
	fields := make(map[string]string, len(value.Files)+len(value.Indexes)+1)
	for _, file := range value.Files {
		raw, err := json.Marshal(file)
		if err != nil {
			return fmt.Errorf("encode run file: %w", err)
		}
		fields["file:"+file.ID] = string(raw)
	}
	for id, index := range value.Indexes {
		raw, err := json.Marshal(index)
		if err != nil {
			return fmt.Errorf("encode run index: %w", err)
		}
		fields["file:"+id] = string(raw)
	}
	value.Files = nil
	value.Indexes = nil
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode run: %w", err)
	}
	fields["context"] = string(raw)
	return wrap(s.Redis.PutHash(ctx, "run:"+value.Run.ID, fields, lifetime))
}

func (s *Service) Close(ctx context.Context, p model.Principal, id string) error {
	if _, err := s.Get(ctx, p, id); err != nil {
		return err
	}
	return wrap(s.Redis.Delete(ctx, "run:"+id))
}

func (s *Service) Event(ctx context.Context, p model.Principal, id, event string) error {
	run, err := s.Get(ctx, p, id)
	if err != nil {
		return err
	}
	if event != "running" {
		return model.ErrInvalid
	}
	if run.Run.Purpose == "review" {
		return nil
	}
	return wrap(s.Repository.RecordRunning(ctx, p.User.ID, run.Run.GameID, s.Now().UnixMilli()))
}

func (s *Service) Resource(ctx context.Context, p model.Principal, id, resourceID string) (Blob, []Blob, error) {
	_, err := s.Get(ctx, p, id)
	if err != nil {
		return Blob{}, nil, err
	}
	raw, err := s.Redis.HashValue(ctx, "run:"+id, "file:"+resourceID, lifetime)
	if err != nil {
		return Blob{}, nil, wrap(err)
	}
	if strings.HasPrefix(raw, "[") {
		var files []Blob
		if err = json.Unmarshal([]byte(raw), &files); err != nil {
			return Blob{}, nil, fmt.Errorf("decode run index: %w", err)
		}
		return Blob{}, files, nil
	}
	var file Blob
	if err = json.Unmarshal([]byte(raw), &file); err != nil {
		return Blob{}, nil, fmt.Errorf("decode run file: %w", err)
	}
	return file, nil, nil
}

func (s *Service) envelope(ctx context.Context,
	run *Context,
	detail model.GameDetail,
	prepared runtimeclient.Prepared,
	save *model.Save,
) error {
	provider, exists := s.Runtime.Providers[prepared.ProviderID]
	if !exists {
		return model.ErrUnavailable
	}
	base := "/runtime/providers/" + provider.ProviderID + "/" + provider.BundleSHA256 + "/"
	run.Run.ProviderModuleURL = base + "client.mjs"
	var config struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(prepared.Config, &config); err != nil {
		return fmt.Errorf("read prepared content: %w", err)
	}
	format := ""
	if prepared.Checkpoint != nil {
		format = prepared.Checkpoint.WriteFormat
	}
	run.Run.Extinfo = model.Extinfo{
		CoreID:           prepared.CoreID,
		ProviderID:       prepared.ProviderID,
		TargetID:         prepared.TargetID,
		CoreFingerprint:  prepared.CoreFingerprint,
		ROMHash:          prepared.ROMHash,
		CheckpointFormat: format,
		RuntimeOptions:   prepared.TargetOptions,
		Content:          config.Content,
	}
	resources, err := s.resources(ctx, run, detail.Files, prepared)
	if err != nil {
		return err
	}
	restore, err := s.restore(ctx, run, save)
	if err != nil {
		return err
	}
	purpose := "PRODUCT"
	returnTo := "/games/" + detail.Game.ID
	if run.Run.Purpose == "review" {
		purpose = "REVIEW_PREVIEW"
		returnTo = "/admin/reviews/" + detail.Game.ID
	}
	value := map[string]any{
		"schemaVersion": 1,
		"session": map[string]any{
			"id":           run.Run.ID,
			"purpose":      purpose,
			"mode":         "SINGLE",
			"title":        detail.Game.Title,
			"platformName": detail.Game.DirectoryName,
			"coreName":     prepared.CoreID,
			"returnTo":     returnTo,
			"warnings":     []string{},
		},
		"runtime": map[string]any{
			"coreId":             prepared.CoreID,
			"coreFingerprint":    prepared.CoreFingerprint,
			"romHash":            prepared.ROMHash,
			"providerId":         provider.ProviderID,
			"providerVersion":    provider.ProviderVersion,
			"providerApiVersion": 1,
			"bundleSha256":       provider.BundleSHA256,
			"targetId":           prepared.TargetID,
			"capabilities":       prepared.Capabilities,
			"checkpoint":         prepared.Checkpoint,
			"moduleUrl":          run.Run.ProviderModuleURL,
			"moduleSha256":       provider.ModuleSHA256,
			"runtimeBaseUrl":     base,
		},
		"resources":     resources,
		"targetOptions": prepared.TargetOptions,
		"restore":       restore,
	}
	run.Run.Envelope, err = json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	return nil
}

func (s *Service) restore(ctx context.Context, run *Context, save *model.Save) (any, error) {
	if save == nil {
		return json.RawMessage("null"), nil
	}
	if run.Run.Purpose != "play" {
		return nil, model.ErrInvalid
	}
	if save.Game.ID != run.Run.GameID || run.Checkpoint == nil {
		return nil, model.ErrInvalid
	}
	result, err := s.Restorable(ctx, true, *save, true, &run.Run.Extinfo, run.Checkpoint.ReadFormats)
	if err != nil {
		return nil, err
	}
	if !result.Restorable {
		return nil, model.ErrConflict
	}
	save.Restorable = true
	save.RestoreReason = ""
	run.Run.Save = save
	return map[string]any{
			"kind":      "HTTP",
			"url":       "/api/v1/saves/" + save.ID + "/payload",
			"format":    save.Extinfo.CheckpointFormat,
			"sha256":    save.PayloadHash,
			"sizeBytes": save.SizeBytes,
		},
		nil
}

type RestoreResult struct {
	Restorable bool    `json:"restorable"`
	Reason     *string `json:"reason"`
}

func (s *Service) Restorable(ctx context.Context,
	gameAvailable bool,
	save model.Save,
	available bool,
	current *model.Extinfo,
	formats []string) (RestoreResult,
	error,
) {
	var result RestoreResult
	err := s.Runtime.Call(ctx,
		"restorable",
		map[string]any{
			"gameAvailable": gameAvailable,
			"saveAvailable": available,
			"current":       current,
			"context":       save.Extinfo,
			"readFormats":   formats,
		},
		&result)
	return result, wrap(err)
}

func blob(file model.GameFile) Blob {
	return Blob{
		ID:          uuid.NewString(),
		Filename:    path.Base(file.LogicalKey),
		Key:         file.StorageKey,
		LogicalPath: file.LogicalKey,
		SHA256:      file.SHA256,
		SizeBytes:   file.SizeBytes,
		MediaType:   mediaType(file.LogicalKey),
	}
}

func mediaType(name string) string {
	value := mime.TypeByExtension(path.Ext(name))
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

func ResourceURL(runID string, file Blob) string {
	return "/api/v1/runs/" + runID + "/resources/" + url.PathEscape(file.ID) + "/" + url.PathEscape(file.Filename)
}

func logicalURLPath(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("run operation: %w", err)
}

func (s *Service) isolation(id string) string {
	return strings.ReplaceAll(s.IsolationTemplate, "{runId}", id)
}
