package scans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

const replacementWindow = 24 * time.Hour

// This executor belongs only to content replacement: one running and two FIFO
// waiting operations. It neither consumes the two scan slots nor queues a
// goroutine per request. Redis retains only the latest operation for each game.
type replacements struct {
	mutex  sync.Mutex
	active *replacementTask
	queue  []*replacementTask
	byGame map[string]*replacementTask
}

type replacementSession struct {
	Progress model.ContentReplacement        `json:"progress"`
	FileID   string                          `json:"fileId,omitempty"`
	Prepared *model.ContentReplacementResult `json:"prepared,omitempty"`
}

type replacementTask struct {
	session    replacementSession
	principal  model.Principal
	sourcePath string
	version    int64
	ctx        context.Context
	cancel     context.CancelFunc
	stop       func() bool
}

func replacementKey(gameID string) string { return "content-replacement:" + gameID }

// Replace admits bounded background preparation and returns before any content
// copy or runtime call. Its worker survives completion of the submission HTTP
// request, but explicit cancellation and service shutdown remain authoritative.
func (s *Service) Replace(ctx context.Context, p model.Principal, id, sourcePath string,
	version int64,
) (model.ContentReplacement, error) {
	if err := p.Admin(); err != nil {
		return model.ContentReplacement{}, wrap(err)
	}
	if version < 1 || !filepath.IsAbs(sourcePath) || len(sourcePath) > 4096 || strings.ContainsRune(sourcePath, 0) {
		return model.ContentReplacement{}, model.ErrInvalid
	}
	old, err := s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	if err != nil {
		return model.ContentReplacement{}, wrap(err)
	}
	if old.Game.Version != version {
		return model.ContentReplacement{}, model.ErrConflict
	}
	directory, err := s.Repository.Directory(ctx, old.Game.PlatformInstanceID)
	if err != nil || !directory.Enabled {
		return model.ContentReplacement{}, errors.Join(model.ErrInvalid, err)
	}
	worker, cancel := context.WithTimeout(s.replacementContext(), ContentPreparationTimeout)
	now := s.Now().UnixMilli()
	task := &replacementTask{
		principal: p, sourcePath: sourcePath, version: version, ctx: worker, cancel: cancel,
		session: replacementSession{Progress: model.ContentReplacement{
			ID: uuid.NewString(), GameID: id, Status: "queued", Phase: "queued", CanCancel: true,
			CreatedAtMs: now, UpdatedAtMs: now, ExpiresAtMs: now + replacementWindow.Milliseconds(),
		}},
	}
	if err = s.enqueueReplacement(ctx, worker, task); err != nil {
		cancel()
		return model.ContentReplacement{}, err
	}
	s.replacements.mutex.Lock()
	progress := task.session.Progress
	s.replacements.mutex.Unlock()
	return progress, nil
}

func (s *Service) replacementContext() context.Context {
	if s.Context != nil {
		return s.Context
	}
	return context.Background()
}

func (s *Service) enqueueReplacement(ctx, lifetime context.Context, task *replacementTask) error {
	state := &s.replacements
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if err := s.replacementContext().Err(); err != nil {
		return model.ErrUnavailable
	}
	if state.byGame[task.session.Progress.GameID] != nil {
		return model.ErrConflict
	}
	if state.active != nil && len(state.queue) >= 2 {
		return model.ErrUnavailable
	}
	if state.active == nil {
		task.session.Progress.Status, task.session.Progress.Phase = "running", "preparing"
	}
	if err := s.saveReplacement(ctx, &task.session); err != nil {
		return err
	}
	if state.byGame == nil {
		state.byGame = make(map[string]*replacementTask)
	}
	state.byGame[task.session.Progress.GameID] = task
	s.workers.Add(1)
	task.stop = context.AfterFunc(lifetime, func() { s.expireReplacement(lifetime, task) })
	if state.active == nil {
		state.active = task
		go s.runReplacement(task)
	} else {
		state.queue = append(state.queue, task)
	}
	return nil
}

func (s *Service) runReplacement(task *replacementTask) {
	err := s.replacementPhase(task.ctx, task, "preparing")
	if err == nil {
		err = s.prepareReplacement(task.ctx, task)
	}
	s.replacements.mutex.Lock()
	s.finishReplacementTask(task.ctx, task, err)
	s.replacements.mutex.Unlock()
}

// finishReplacementTask holds the replacement mutex. The prepared file ID is
// retained before CAS so a lost final Redis write or a process restart can
// reconcile the publication without re-running preparation.
func (s *Service) finishReplacementTask(ctx context.Context, task *replacementTask, err error) {
	if err == nil {
		task.session.Progress.Status, task.session.Progress.Phase = "completed", "completed"
		task.session.Progress.Result = task.session.Prepared
	} else {
		s.replacementFailure(task, err)
		slog.Warn("content replacement stopped", "operationId", task.session.Progress.ID, "error", err)
	}
	task.session.Progress.CanCancel = false
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if saveErr := s.saveReplacement(bounded, &task.session); saveErr != nil {
		slog.Error("write content replacement result", "operationId", task.session.Progress.ID, "error", saveErr)
	}
	task.stop()
	task.cancel()
	delete(s.replacements.byGame, task.session.Progress.GameID)
	s.workers.Done()
	if s.replacements.active == task {
		s.replacements.active = nil
		if len(s.replacements.queue) > 0 {
			next := s.replacements.queue[0]
			s.replacements.queue = s.replacements.queue[1:]
			s.replacements.active = next
			go s.runReplacement(next)
		}
	}
}

func (s *Service) replacementFailure(task *replacementTask, err error) {
	progress := &task.session.Progress
	progress.Status = "failed"
	switch {
	case errors.Is(err, persistence.ErrCommitUncertain):
		progress.Status = "interrupted"
		progress.Error = replacementError("CONTENT_REPLACEMENT_COMMIT_UNCERTAIN", "发布结果尚未确认，请刷新游戏资料核对后再操作。")
	case errors.Is(err, context.DeadlineExceeded):
		progress.Error = replacementError("CONTENT_REPLACEMENT_TIMEOUT", "内容替换超过执行时限，请检查来源后重试。")
	case s.replacementContext().Err() != nil:
		progress.Status = "interrupted"
		progress.Error = replacementError("CONTENT_REPLACEMENT_INTERRUPTED", "服务已停止，内容替换未继续执行，请核对游戏资料后重试。")
	case errors.Is(err, context.Canceled):
		progress.Status, progress.Error = "cancelled", nil
	case errors.Is(err, model.ErrConflict):
		progress.Error = replacementError("VERSION_CONFLICT", "游戏资料已变化，原活动内容保留，请刷新后重试。")
	case errors.Is(err, model.ErrStorageFull):
		progress.Error = replacementError("INSUFFICIENT_STORAGE", "可用存储空间不足，原活动内容保留。")
	case errors.Is(err, model.ErrUnavailable) && strings.Contains(err.Error(), "RUNTIME_BUSY"):
		progress.Error = replacementError("RUNTIME_BUSY", "内容转换正在繁忙，请稍后重试。原活动内容保留。")
	default:
		var contentError *model.RuntimeContentError
		if errors.As(err, &contentError) {
			progress.Error = replacementError(contentError.Code, contentError.Message)
		} else {
			progress.Error = replacementError("CONTENT_REPLACEMENT_FAILED", "内容替换失败，原活动内容保留，请检查来源后重试。")
		}
	}
}

func replacementError(code, message string) *model.ContentReplacementError {
	return &model.ContentReplacementError{Code: code, Message: message}
}

func (s *Service) replacementPhase(ctx context.Context, task *replacementTask, phase string) error {
	s.replacements.mutex.Lock()
	defer s.replacements.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return wrap(err)
	}
	task.session.Progress.Status, task.session.Progress.Phase = "running", phase
	return s.saveReplacement(ctx, &task.session)
}

func (s *Service) beginReplacementPublication(ctx context.Context, task *replacementTask,
	fileID string, result model.ContentReplacementResult,
) error {
	s.replacements.mutex.Lock()
	defer s.replacements.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return wrap(err)
	}
	task.session.Progress.Phase, task.session.Progress.CanCancel = "publishing", false
	task.session.FileID, task.session.Prepared = fileID, &result
	return s.saveReplacement(ctx, &task.session)
}

func (s *Service) saveReplacement(ctx context.Context, session *replacementSession) error {
	session.Progress.UpdatedAtMs = s.Now().UnixMilli()
	session.Progress.ExpiresAtMs = session.Progress.UpdatedAtMs + replacementWindow.Milliseconds()
	value, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode content replacement: %w", err)
	}
	return wrap(s.Redis.Set(ctx, replacementKey(session.Progress.GameID), string(value), replacementWindow))
}

// ContentReplacement reads current or exact-ID progress. A later replacement or
// TTL expiry makes an old operation ID unavailable; no historical job list exists.
func (s *Service) ContentReplacement(ctx context.Context, p model.Principal, gameID, operationID string,
) (model.ContentReplacement, error) {
	if err := p.Admin(); err != nil {
		return model.ContentReplacement{}, wrap(err)
	}
	s.replacements.mutex.Lock()
	defer s.replacements.mutex.Unlock()
	session, err := s.replacementSession(ctx, gameID, operationID)
	return session.Progress, err
}

func (s *Service) replacementSession(ctx context.Context, gameID, operationID string) (replacementSession, error) {
	if task := s.replacements.byGame[gameID]; task != nil {
		if operationID != "" && operationID != task.session.Progress.ID {
			return replacementSession{}, model.ErrNotFound
		}
		return task.session, nil
	}
	value, err := s.Redis.Get(ctx, replacementKey(gameID))
	if errors.Is(err, model.ErrContextExpired) {
		return replacementSession{}, model.ErrNotFound
	}
	if err != nil {
		return replacementSession{}, wrap(err)
	}
	var session replacementSession
	if err = json.Unmarshal([]byte(value), &session); err != nil {
		return session, wrap(err)
	}
	if session.Progress.GameID != gameID || operationID != "" && operationID != session.Progress.ID {
		return session, model.ErrNotFound
	}
	uncertain := session.Progress.Error != nil && session.Progress.Error.Code == "CONTENT_REPLACEMENT_COMMIT_UNCERTAIN"
	if model.OneOf(session.Progress.Status, "queued", "running", "canceling") || uncertain {
		if err = s.reconcileReplacement(ctx, &session); err != nil {
			return session, err
		}
	}
	return session, nil
}

func (s *Service) reconcileReplacement(ctx context.Context, session *replacementSession) error {
	session.Progress.Status, session.Progress.CanCancel = "interrupted", false
	session.Progress.Error = replacementError("CONTENT_REPLACEMENT_INTERRUPTED", "服务已重启，内容替换未继续执行，请核对游戏资料后重试。")
	if session.FileID != "" {
		committed, err := s.Repository.ParentCommitted(ctx, session.Progress.GameID, session.FileID)
		if err != nil {
			return wrap(err)
		}
		if committed {
			session.Progress.Status, session.Progress.Phase = "completed", "completed"
			session.Progress.Result, session.Progress.Error = session.Prepared, nil
		}
	}
	return s.saveReplacement(ctx, session)
}

func (s *Service) CancelReplacement(ctx context.Context, p model.Principal, gameID, operationID string,
) (model.ContentReplacement, error) {
	if err := p.Admin(); err != nil {
		return model.ContentReplacement{}, wrap(err)
	}
	s.replacements.mutex.Lock()
	defer s.replacements.mutex.Unlock()
	session, err := s.replacementSession(ctx, gameID, operationID)
	if err != nil || !session.Progress.CanCancel {
		return session.Progress, err
	}
	task := s.replacements.byGame[gameID]
	task.cancel()
	if s.replacements.active == task {
		task.session.Progress.Status, task.session.Progress.CanCancel = "canceling", false
		return task.session.Progress, s.saveReplacement(ctx, &task.session)
	}
	s.removeQueuedReplacement(task)
	s.finishReplacementTask(ctx, task, context.Canceled)
	return task.session.Progress, nil
}

func (s *Service) removeQueuedReplacement(task *replacementTask) {
	for i, queued := range s.replacements.queue {
		if queued == task {
			s.replacements.queue = append(s.replacements.queue[:i], s.replacements.queue[i+1:]...)
			return
		}
	}
}

func (s *Service) expireReplacement(ctx context.Context, task *replacementTask) {
	s.replacements.mutex.Lock()
	defer s.replacements.mutex.Unlock()
	if s.replacements.byGame[task.session.Progress.GameID] != task || s.replacements.active == task {
		return
	}
	s.removeQueuedReplacement(task)
	s.finishReplacementTask(ctx, task, task.ctx.Err())
}
