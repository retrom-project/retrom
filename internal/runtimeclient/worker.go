package runtimeclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"retrom/internal/model"
)

const (
	maximumMessage = 64 * 1024 * 1024
	maximumPending = 32
)

var errOutputLimit = errors.New("runtime message exceeds limit")

type workerPool struct {
	context   context.Context
	cancel    context.CancelFunc
	root      string
	node      string
	admission chan struct{}
	sequence  atomic.Uint64
	mutex     sync.Mutex
	process   *sharedProcess
}

type runtimeResponse struct {
	ID           string            `json:"id"`
	Result       json.RawMessage   `json:"result"`
	Error        string            `json:"error"`
	ErrorDetails map[string]string `json:"errorDetails"`
	failure      error
}

type sharedProcess struct {
	owner   *workerPool
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	writes  chan []byte
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	mutex   sync.Mutex
	pending map[string]chan runtimeResponse
	failure error
}

func newWorkerPool(ctx context.Context, root, node string) *workerPool {
	lifetime, cancel := context.WithCancel(ctx)
	return &workerPool{
		context: lifetime, cancel: cancel, root: root, node: node,
		admission: make(chan struct{}, maximumPending),
	}
}

func (p *workerPool) Close() {
	p.cancel()
	p.mutex.Lock()
	process := p.process
	p.mutex.Unlock()
	if process != nil {
		process.fail(fmt.Errorf("runtime stopped: %w", model.ErrUnavailable))
		<-process.stopped
	}
}

func (p *workerPool) Call(ctx context.Context, command string, input, output any) error {
	bounded, cancel := context.WithTimeout(ctx, commandTimeout(command))
	defer cancel()
	select {
	case p.admission <- struct{}{}:
	case <-bounded.Done():
		return fmt.Errorf("runtime admission: %w", bounded.Err())
	case <-p.context.Done():
		return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
	defer func() { <-p.admission }()
	if input == nil {
		input = map[string]any{}
	}
	id := strconv.FormatUint(p.sequence.Add(1), 10)
	encoded, err := json.Marshal(map[string]any{"id": id, "command": command, "input": input})
	if err != nil {
		return fmt.Errorf("runtime input: %w", err)
	}
	if len(encoded) > maximumMessage {
		return errOutputLimit
	}
	process, err := p.current()
	if err != nil {
		return err
	}
	if err := process.exchange(bounded, id, encoded, output); err != nil {
		return fmt.Errorf("runtime %s: %w", command, err)
	}
	return nil
}

func (process *sharedProcess) exchange(ctx context.Context, id string, encoded []byte, output any) error {
	var err error
	completed := make(chan runtimeResponse, 1)
	process.mutex.Lock()
	if process.failure != nil {
		err = process.failure
	} else {
		process.pending[id] = completed
	}
	process.mutex.Unlock()
	if err != nil {
		return err
	}
	defer process.forget(id)
	select {
	case process.writes <- append(encoded, '\n'):
	case <-process.done:
		return process.reason()
	case <-ctx.Done():
		return fmt.Errorf("runtime request cancelled: %w", ctx.Err())
	}
	select {
	case response := <-completed:
		if response.failure != nil {
			return response.failure
		}
		if response.Error != "" {
			return runtimeFailure(response.Error, response.ErrorDetails)
		}
		if err := json.Unmarshal(response.Result, output); err != nil {
			return fmt.Errorf("runtime result: %w", err)
		}
		return nil
	case <-ctx.Done():
		process.forget(id)
		process.cancelRequest(id)
		return fmt.Errorf("runtime request cancelled: %w", ctx.Err())
	case <-process.owner.context.Done():
		return fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
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

func (p *workerPool) current() (*sharedProcess, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.context.Err() != nil {
		return nil, fmt.Errorf("runtime stopped: %w", model.ErrUnavailable)
	}
	if p.process != nil {
		return p.process, nil
	}
	command := exec.CommandContext(p.context, p.node, filepath.Join(p.root, "scripts/runtime-cli.mjs"), "--serve")
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("runtime input pipe: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("runtime output pipe: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("start runtime CLI: %w", err)
	}
	process := &sharedProcess{
		owner: p, command: command, input: input, output: bufio.NewReaderSize(output, 64*1024),
		writes: make(chan []byte, maximumPending*2), done: make(chan struct{}),
		stopped: make(chan struct{}), pending: make(map[string]chan runtimeResponse),
	}
	p.process = process
	go process.writeRequests()
	go process.readResponses()
	return process, nil
}

func (p *sharedProcess) forget(id string) {
	p.mutex.Lock()
	delete(p.pending, id)
	p.mutex.Unlock()
}

func (p *sharedProcess) cancelRequest(id string) {
	encoded, _ := json.Marshal(map[string]string{"cancel": id})
	select {
	case p.writes <- append(encoded, '\n'):
	case <-p.done:
	default:
		p.fail(fmt.Errorf("runtime write queue stalled: %w", model.ErrUnavailable))
	}
}

func (p *sharedProcess) reason() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.failure
}

func (p *sharedProcess) fail(err error) {
	p.once.Do(func() {
		p.mutex.Lock()
		p.failure = err
		for id, completed := range p.pending {
			completed <- runtimeResponse{failure: err}
			delete(p.pending, id)
		}
		close(p.done)
		p.mutex.Unlock()
		p.owner.mutex.Lock()
		if p.owner.process == p {
			p.owner.process = nil
		}
		p.owner.mutex.Unlock()
		_ = p.command.Process.Kill()
		_ = p.input.Close()
	})
}

func (p *sharedProcess) writeRequests() {
	for {
		select {
		case <-p.done:
			return
		case encoded := <-p.writes:
			if _, err := p.input.Write(encoded); err != nil {
				p.fail(fmt.Errorf("write runtime request: %w", err))
				return
			}
		}
	}
}

func (p *sharedProcess) readResponses() {
	defer close(p.stopped)
	// Wait only after stdout is drained; Wait closes the pipe otherwise.
	defer func() { _ = p.command.Wait() }()
	for {
		raw, err := readMessage(p.output)
		if err != nil {
			p.fail(fmt.Errorf("runtime transport: %w", err))
			return
		}
		var response runtimeResponse
		if err := json.Unmarshal(raw, &response); err != nil || response.ID == "" ||
			(response.Error == "" && len(response.Result) == 0) || (response.Error != "" && len(response.Result) > 0) {
			p.fail(fmt.Errorf("runtime response invalid: %w", model.ErrUnavailable))
			return
		}
		sequence, sequenceErr := strconv.ParseUint(response.ID, 10, 64)
		if sequenceErr != nil || sequence == 0 || sequence > p.owner.sequence.Load() {
			p.fail(fmt.Errorf("runtime response correlation: %w", model.ErrUnavailable))
			return
		}
		p.mutex.Lock()
		completed := p.pending[response.ID]
		delete(p.pending, response.ID)
		if completed != nil {
			completed <- response
		}
		p.mutex.Unlock()
		// Responses from cancelled requests are deliberately discarded.
	}
}

func runtimeFailure(code string, details map[string]string) error {
	switch code {
	case "RUNTIME_PARENT_MISSING":
		name := details["parents"]
		if name == "" {
			name = details["logicalKey"]
		}
		return &model.RuntimeContentError{Code: code, Message: "Required Parent ROM is missing: " + name}
	case "RUNTIME_PARENT_INVALID":
		return &model.RuntimeContentError{
			Code: code, Message: "Select a Parent archive for this core; the entry archive cannot be replaced here",
		}
	}
	if failure := ContentFailure(code); failure != nil {
		return failure
	}
	category := model.ErrInvalid
	switch code {
	case "RUNTIME_BUSY", "RUNTIME_CANCELLED":
		category = model.ErrUnavailable
	case "CORE_UNAVAILABLE", "CONTENT_CHANGED", "FORMAT_UNREADABLE":
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
