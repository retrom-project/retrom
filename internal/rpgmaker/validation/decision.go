package validation

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type Decision string

const (
	DecisionPass Decision = "PASS"
	DecisionFail Decision = "FAIL"
)

func (machine *Machine) Decide(decision Decision, note string) (string, error) {
	if machine.State != StateAwaitingDecision {
		return "", ErrInvalidState
	}
	normalized, err := NormalizeDecisionNote(note)
	if err != nil || decision != DecisionPass && decision != DecisionFail ||
		decision == DecisionFail && normalized == "" {
		return "", ErrDecisionInvalid
	}
	if decision == DecisionPass {
		machine.State = StatePassed
	} else {
		machine.State = StateFailed
		machine.FailureCode = "RPG_RUNTIME_REVIEWER_FAILED"
	}
	return normalized, nil
}

func NormalizeDecisionNote(value string) (string, error) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	value = strings.TrimSpace(norm.NFC.String(value))
	if utf8.RuneCountInString(value) > 500 || len(value) > 2000 {
		return "", ErrDecisionInvalid
	}
	for _, current := range value {
		if current <= 0x08 || current == 0x0b || current == 0x0c ||
			current >= 0x0e && current <= 0x1f || current >= 0x7f && current <= 0x9f {
			return "", ErrDecisionInvalid
		}
	}
	return value, nil
}
