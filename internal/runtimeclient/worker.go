package runtimeclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"retrom/internal/model"
)

const maximumMessage = 64 * 1024 * 1024

var errOutputLimit = errors.New("runtime message exceeds limit")

type workerPool struct {
	context   context.Context
	cancel    context.CancelFunc
	available chan *runtimeWorker
	workers   []*runtimeWorker
}

type runtimeWorker struct {
	mutex   sync.Mutex
	process *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	root    string
	node    string
}

func newWorkerPool(ctx context.Context, root, node string) *workerPool {
	lifetime, cancel := context.WithCancel(ctx)
	pool := &workerPool{context: lifetime, cancel: cancel, available: make(chan *runtimeWorker, 4)}
	for range 4 {
		worker := &runtimeWorker{root: root, node: node}
		pool.workers = append(pool.workers, worker)
		pool.available <- worker
	}
	return pool
}

func (p *workerPool) Close() {
	p.cancel()
	for _, worker := range p.workers {
		worker.mutex.Lock()
		worker.stop()
		worker.mutex.Unlock()
	}
}

func (p *workerPool) Call(ctx context.Context, command string, input, output any) error {
	bounded, cancel := context.WithTimeout(ctx, commandTimeout(command))
	defer cancel()
	var worker *runtimeWorker
	select {
	case worker = <-p.available:
	case <-bounded.Done():
		return fmt.Errorf("runtime admission: %w", bounded.Err())
	case <-p.context.Done():
		return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
	defer func() { p.available <- worker }()
	return worker.call(bounded, p.context, command, input, output)
}

func commandTimeout(command string) time.Duration {
	switch command {
	case "detect-scummvm":
		return time.Minute
	case "assemble-resource", "parent-archive", "normalize-content":
		return 2 * time.Minute
	default:
		return 30 * time.Second
	}
}

func (w *runtimeWorker) call(ctx, lifetime context.Context, command string, input, output any) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if input == nil {
		input = map[string]any{}
	}
	encoded, err := json.Marshal(map[string]any{"id": "request", "command": command, "input": input})
	if err != nil {
		return fmt.Errorf("runtime input: %w", err)
	}
	if len(encoded) > maximumMessage {
		return errOutputLimit
	}
	if w.process == nil {
		if err = w.start(lifetime); err != nil {
			return err
		}
	}
	completed := make(chan error, 1)
	writer, reader := w.input, w.output
	go func() { completed <- exchange(writer, reader, encoded, output) }()
	select {
	case err = <-completed:
		if err != nil && !errors.Is(err, model.ErrInvalid) && !errors.Is(err, model.ErrConflict) {
			w.stop()
		}
		return err
	case <-ctx.Done():
		w.kill()
		<-completed
		w.stop()
		return fmt.Errorf("runtime %s: %w", command, ctx.Err())
	}
}

func (w *runtimeWorker) start(ctx context.Context) error {
	process := exec.CommandContext(ctx, w.node, filepath.Join(w.root, "scripts/runtime-cli.mjs"), "--serve")
	input, err := process.StdinPipe()
	if err != nil {
		return fmt.Errorf("runtime input pipe: %w", err)
	}
	output, err := process.StdoutPipe()
	if err != nil {
		return fmt.Errorf("runtime output pipe: %w", err)
	}
	process.Stderr = io.Discard
	if err = process.Start(); err != nil {
		return fmt.Errorf("start runtime worker: %w", err)
	}
	w.process, w.input, w.output = process, input, bufio.NewReaderSize(output, 64*1024)
	return nil
}

func (w *runtimeWorker) stop() {
	if w.process == nil {
		return
	}
	w.kill()
	if err := w.process.Wait(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			slog.Error("wait runtime worker", "error", err)
		}
	}
	w.process, w.input, w.output = nil, nil, nil
}

func (w *runtimeWorker) kill() {
	if w.process == nil || w.process.Process == nil {
		return
	}
	if err := w.process.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		slog.Error("stop runtime worker", "error", err)
	}
}

func exchange(input io.Writer, reader *bufio.Reader, encoded []byte, output any) error {
	if _, err := input.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write runtime request: %w", err)
	}
	raw, err := readMessage(reader)
	if err != nil {
		return err
	}
	var response struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("decode runtime response: %w", err)
	}
	if response.ID != "request" {
		return fmt.Errorf("runtime response correlation: %w", model.ErrUnavailable)
	}
	if response.Error != "" {
		return runtimeFailure(response.Error)
	}
	if err = json.Unmarshal(response.Result, output); err != nil {
		return fmt.Errorf("runtime result: %w", err)
	}
	return nil
}

func runtimeFailure(code string) error {
	category := model.ErrInvalid
	switch code {
	case "CORE_UNAVAILABLE", "CORE_CHANGED", "CONTENT_CHANGED", "FORMAT_UNREADABLE":
		category = model.ErrConflict
	}
	return fmt.Errorf("runtime rejected input: %w (%s)", category, code)
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	var result bytes.Buffer
	for {
		part, err := reader.ReadSlice('\n')
		if result.Len()+len(part) > maximumMessage {
			return nil, errOutputLimit
		}
		result.Write(part)
		if err == nil {
			return result.Bytes(), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("read runtime response: %w", err)
		}
	}
}

func (c *Client) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}
