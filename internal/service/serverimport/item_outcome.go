package serverimport

import (
	"context"
	"encoding/json"
	"fmt"
)

func (service *Outcomes) CompleteItem(
	ctx context.Context,
	unit Work,
	requirementID, state string,
	candidate *EvaluatedCandidate,
	code string,
) error {
	plan, err := itemOutcome(unit, requirementID, state, candidate, code, service.now().UnixMilli())
	if err != nil {
		return err
	}
	err = service.repository.WithWrite(ctx, func(scope OutcomeScope) error {
		if err := scope.Write.Lock(ctx, unit, plan.Now, RunningWorker); err != nil {
			return fmt.Errorf("lock item outcome: %w", err)
		}
		if err := scope.Write.Item(ctx, plan); err != nil {
			return fmt.Errorf("write item outcome: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete import item: %w", err)
	}
	return nil
}

func itemOutcome(
	unit Work,
	requirementID, state string,
	candidate *EvaluatedCandidate,
	code string,
	now int64,
) (ItemOutcome, error) {
	plan := ItemOutcome{Unit: unit, RequirementID: requirementID, State: state, Code: code, Now: now}
	if candidate != nil {
		if candidate.Item.RequirementID != requirementID || candidate.Static == nil && candidate.DAT == nil {
			return ItemOutcome{}, ErrCatalogInvalid
		}
		_, method := SelectedStatus(candidate)
		plan.Method = &method
		details, err := json.Marshal(candidate.Details)
		if err != nil {
			return ItemOutcome{}, fmt.Errorf("encode item outcome evidence: %w", err)
		}
		plan.Details = details
		id, candidateState := candidate.ID, candidate.State
		if state == "SOURCE_CHANGED" {
			candidateState = "SOURCE_CHANGED"
		}
		plan.CandidateID, plan.CandidateState = &id, &candidateState
	}
	event, err := json.Marshal(map[string]any{"schemaVersion": 1, "phase": "INSTALLING", "result": state})
	if err != nil {
		return ItemOutcome{}, fmt.Errorf("encode item outcome event: %w", err)
	}
	plan.Event = event
	return plan, nil
}
