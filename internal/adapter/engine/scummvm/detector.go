// Package scummvm executes the verified native upstream detector against private input trees.
package scummvm

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	scummvmpolicy "retrom/internal/capability/engine/scummvm"
	"retrom/internal/foundation/cleanup"
	model "retrom/internal/model/libraryimport"
)

// Tool is resolved from a verified Provider installation; callers must not take
// the executable path, commit or enabled engines from an upload or API request.
type Tool struct {
	Path           string
	UpstreamCommit string
	Engines        []string
}

var _ model.ScummVMDetector = (*Detector)(nil)

type Detector struct {
	resolve func(context.Context) (Tool, error)
}

func New(resolve func(context.Context) (Tool, error)) *Detector {
	return &Detector{resolve: resolve}
}

// Detect accepts a private materialized tree owned by the import operation. The
// caller keeps it immutable until this function returns and removes it afterward.
func (detector *Detector) Detect(ctx context.Context, root, sourceDigest string) (scummvmpolicy.Result, error) {
	if !scummvmpolicy.ValidDigest(sourceDigest, 32) {
		return scummvmpolicy.Result{}, model.ErrScummVMInputInvalid
	}
	if err := checkTree(ctx, root); err != nil {
		return scummvmpolicy.Result{}, err
	}
	tool, err := detector.resolve(ctx)
	if err != nil {
		return scummvmpolicy.Result{}, fmt.Errorf("scummvm/resolve: %w", err)
	}
	if !filepath.IsAbs(tool.Path) || !scummvmpolicy.ValidDigest(tool.UpstreamCommit, 20) || len(tool.Engines) == 0 {
		return scummvmpolicy.Result{}, model.ErrScummVMToolFailed
	}
	output, err := runDetector(ctx, tool.Path, root)
	if err != nil {
		return scummvmpolicy.Result{}, err
	}
	result, err := scummvmpolicy.ParseResult(output, tool.UpstreamCommit, tool.Engines, sourceDigest)
	if err != nil {
		// The parser's sole error is the shared sentinel; preserve its bare identity.
		return result, scummvmpolicy.ErrResultInvalid
	}
	return result, nil
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
		return nil, model.ErrScummVMLimit
	}
	if err != nil {
		return nil, model.ErrScummVMToolFailed
	}
	return stdout.data, nil
}

func checkTree(ctx context.Context, root string) error {
	if !filepath.IsAbs(root) {
		return model.ErrScummVMInputInvalid
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return model.ErrScummVMInputInvalid
	}
	count := 0
	err = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return model.ErrScummVMInputInvalid
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scummvm/input-canceled: %w", err)
		}
		count++
		if count > 100000 {
			return model.ErrScummVMLimit
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
			return remaining, model.ErrScummVMLimit
		}
	}
	return len(value), nil
}
