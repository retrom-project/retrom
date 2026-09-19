package architecture

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrInvalidBaseline   = errors.New("architecture: baseline must be a full commit SHA")
	ErrUnrelatedBaseline = errors.New("architecture: HEAD does not descend from the required baseline")
)

// BaselineReport identifies the baseline relationship without changing any refs.
type BaselineReport struct {
	Baseline string `json:"baseline"`
	Commit   string `json:"commit"`
	Tree     string `json:"tree"`
	Relation string `json:"relation"`
}

// InspectBaseline verifies ancestry rather than trusting a branch name.
func InspectBaseline(ctx context.Context, root, baseline string) (BaselineReport, error) {
	if !validCommitSHA(baseline) {
		return BaselineReport{}, ErrInvalidBaseline
	}
	commit, err := inventoryGit(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return BaselineReport{}, err
	}
	tree, err := inventoryGit(ctx, root, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return BaselineReport{}, err
	}
	if _, err := inventoryGit(ctx, root, "merge-base", "--is-ancestor", baseline, "HEAD"); err != nil {
		return BaselineReport{}, errors.Join(ErrUnrelatedBaseline, err)
	}
	relation := "DESCENDANT"
	if strings.TrimSpace(commit) == baseline {
		relation = "BASELINE"
	}
	return BaselineReport{
		Baseline: baseline, Commit: strings.TrimSpace(commit), Tree: strings.TrimSpace(tree), Relation: relation,
	}, nil
}

func validCommitSHA(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func inventoryGit(ctx context.Context, root string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("architecture git %s: %w", arguments[0], err)
	}
	return string(output), nil
}
