package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	jobsmodel "retrom/internal/model/jobs"
)

func (server *Server) streamJobEvents(writer http.ResponseWriter, request *http.Request, jobID string) {
	snapshot, maximum, err := server.jobService.JobStreamSnapshot(request.Context(), jobID)
	if !server.streamSnapshotResult(writer, request, err) {
		return
	}
	encoded, _ := json.Marshal(snapshot)
	server.startEventStream(writer, request, maximum, encoded,
		func(ctx context.Context, after int64) (jobsmodel.EventBatch, error) {
			return server.jobService.JobEvents(ctx, jobID, after)
		})
}

func (server *Server) streamAggregateEvents(writer http.ResponseWriter, request *http.Request, importJobID string) {
	snapshot, maximum, err := server.jobService.ImportStreamSnapshot(request.Context(), importJobID)
	if !server.streamSnapshotResult(writer, request, err) {
		return
	}
	encoded, _ := json.Marshal(snapshot)
	server.startEventStream(writer, request, maximum, encoded,
		func(ctx context.Context, after int64) (jobsmodel.EventBatch, error) {
			return server.jobService.ImportEvents(ctx, importJobID, after)
		})
}

func (server *Server) streamSnapshotResult(writer http.ResponseWriter, request *http.Request, err error) bool {
	if errors.Is(err, jobsmodel.ErrNotFound) {
		server.notFound(writer, request)
		return false
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	return true
}

func (server *Server) startEventStream(writer http.ResponseWriter, request *http.Request, maximum int64,
	snapshot []byte, read func(context.Context, int64) (jobsmodel.EventBatch, error),
) {
	cursor, valid := parseEventCursor(request, maximum)
	if !valid {
		writeError(writer, request, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "事件游标无效", map[string]any{})
		return
	}
	server.streamEvents(writer, request, cursor, maximum, snapshot, read)
}

func parseEventCursor(request *http.Request, maximum int64) (int64, bool) {
	raw := request.Header.Get("Last-Event-ID")
	if raw == "" {
		return maximum, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	return parsed, err == nil && parsed >= 0 && parsed <= maximum && strconv.FormatInt(parsed, 10) == raw
}

func (server *Server) streamEvents(
	writer http.ResponseWriter,
	request *http.Request,
	cursor, snapshotID int64,
	snapshot []byte,
	read func(context.Context, int64) (jobsmodel.EventBatch, error),
) {
	if _, ok := writer.(http.Flusher); !ok {
		writeError(
			writer,
			request,
			http.StatusInternalServerError,
			"STREAMING_UNAVAILABLE",
			"服务器不支持事件流",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("X-Accel-Buffering", "no")
	if request.Header.Get("Last-Event-ID") == "" {
		// snapshot is JSON-encoded by the server, so embedded CR/LF bytes are escaped before reaching SSE framing.
		payload := fmt.Sprintf(
			"id: %d\nevent: snapshot\ndata: %s\n\n", snapshotID, snapshot,
		)
		if err := server.writeSSE(writer, payload); err != nil {
			return
		}
	}
	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(server.sseHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		batch, err := read(request.Context(), cursor)
		if err != nil {
			return
		}
		cursor, err = server.writeEventBatch(writer, cursor, batch.Events)
		if err != nil {
			return
		}

		if batch.Terminal {
			return
		}
		if len(batch.Events) == jobsmodel.EventBatchSize && request.Context().Err() == nil {
			continue
		}
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			if err := server.writeSSE(writer, ": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}

func (server *Server) writeSSE(writer http.ResponseWriter, payload string) error {
	controller := http.NewResponseController(writer)
	deadlineErr := controller.SetWriteDeadline(server.now().Add(30 * time.Second))
	if deadlineErr != nil && !errors.Is(deadlineErr, errors.ErrUnsupported) {
		return fmt.Errorf("set SSE write deadline: %w", deadlineErr)
	}
	if _, err := writer.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write SSE: %w", err)
	}
	if err := controller.Flush(); err != nil {
		return fmt.Errorf("flush SSE: %w", err)
	}
	return nil
}

func (server *Server) writeEventBatch(
	writer http.ResponseWriter,
	cursor int64,
	events []jobsmodel.Event,
) (int64, error) {
	var payload strings.Builder
	for _, event := range events {
		_, _ = fmt.Fprintf(&payload, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, strings.ToLower(event.Type), event.Data)
		cursor = event.ID
	}
	if payload.Len() > 0 {
		if err := server.writeSSE(writer, payload.String()); err != nil {
			return cursor, err
		}
	}
	return cursor, nil
}
