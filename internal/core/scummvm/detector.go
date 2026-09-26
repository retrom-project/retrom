package scummvm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"retrom/internal/cleanup"
)

var (
	ErrToolFailed   = errors.New("SCUMMVM_DETECTION_FAILED")
	ErrInputInvalid = errors.New("SCUMMVM_DETECTION_INPUT_INVALID")
	ErrLimit        = errors.New("SCUMMVM_DETECTION_LIMIT_EXCEEDED")
)

// Tool is resolved from a verified Provider installation; callers must not take
// the executable path, commit or enabled engines from an upload or API request.
type Tool struct {
	Path           string
	UpstreamCommit string
	Engines        []string
}

type Detector struct {
	resolve func(context.Context) (Tool, error)
}

func New(resolve func(context.Context) (Tool, error)) *Detector {
	return &Detector{resolve: resolve}
}

// Detect accepts a private materialized tree owned by the import operation. The
// caller keeps it immutable until this function returns and removes it afterward.
func (detector *Detector) Detect(ctx context.Context, root, sourceDigest string) (Result, error) {
	if !validDigest(sourceDigest, 32) {
		return Result{}, ErrInputInvalid
	}
	if err := checkTree(ctx, root); err != nil {
		return Result{}, err
	}
	tool, err := detector.resolve(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("scummvm/resolve: %w", err)
	}
	if !filepath.IsAbs(tool.Path) || !validDigest(tool.UpstreamCommit, 20) || len(tool.Engines) == 0 {
		return Result{}, ErrToolFailed
	}
	output, err := runDetector(ctx, tool.Path, root)
	if err != nil {
		return Result{}, err
	}
	return parseResult(output, tool, sourceDigest)
}

func runDetector(ctx context.Context, executable, root string) ([]byte, error) {
	temporary, err := os.MkdirTemp("", "retrom-scummvm-")
	if err != nil {
		return nil, fmt.Errorf("scummvm/temporary: %w", err)
	}
	defer func() { cleanup.Error("scummvm-temporary", os.RemoveAll(temporary)) }()
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, executable, "--config=/dev/null", "--retrom-detect", "--recursive",
		"--path="+root, "--savepath="+temporary)
	command.Dir = temporary
	command.Env = []string{"LANG=C.UTF-8", "HOME=" + temporary, "XDG_CONFIG_HOME=" + temporary}
	stdout := &limitedOutput{limit: 16 * 1024 * 1024}
	stderr := &limitedOutput{limit: 64 * 1024, discardOverflow: true}
	command.Stdout, command.Stderr = stdout, stderr
	err = command.Run()
	if bounded.Err() != nil {
		return nil, fmt.Errorf("scummvm/detect: %w", bounded.Err())
	}
	if stdout.exceeded {
		return nil, ErrLimit
	}
	if err != nil {
		return nil, ErrToolFailed
	}
	return stdout.data, nil
}

func checkTree(ctx context.Context, root string) error {
	if !filepath.IsAbs(root) {
		return ErrInputInvalid
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return ErrInputInvalid
	}
	count := 0
	err = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return ErrInputInvalid
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scummvm/input-canceled: %w", err)
		}
		count++
		if count > 100000 {
			return ErrLimit
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scummvm/input: %w", err)
	}
	return nil
}

type limitedOutput struct {
	data            []byte
	limit           int
	exceeded        bool
	discardOverflow bool
}

func (output *limitedOutput) Write(value []byte) (int, error) {
	remaining := output.limit - len(output.data)
	output.data = append(output.data, value[:min(remaining, len(value))]...)
	if len(value) > remaining {
		output.exceeded = true
		if !output.discardOverflow {
			return remaining, ErrLimit
		}
	}
	return len(value), nil
}
