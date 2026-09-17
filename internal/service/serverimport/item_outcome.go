package serverimport

import (
	"context"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/serverimport"
)

func (service *Outcomes) CompleteItem(
	ctx context.Context,
	unit model.Work,
	requirementID, state string,
	candidate *EvaluatedCandidate,
	code string,
) error {
	plan, err := itemOutcome(
		unit, requirementID, state, candidate, code,
		service.now().UnixMilli(),
	)
	if err != nil {
		return err
	}
	err = service.repository.CommitItemOutcome(ctx, plan)
	if err != nil {
		return fmt.Errorf("complete import item: %w", err)
	}
	return nil
}

func itemOutcome(
	unit model.Work,
	requirementID, state string,
	candidate *EvaluatedCandidate,
	code string,
	now int64,
) (model.ItemOutcome, error) {
	plan := model.ItemOutcome{
		Unit: unit, RequirementID: requirementID,
		State: state, Code: code, Now: now,
	}
	if candidate != nil {
		if candidate.Item.RequirementID != requirementID ||
			candidate.Static == nil && candidate.DAT == nil {
			return model.ItemOutcome{}, model.ErrCatalogInvalid
		}
		_, method := SelectedStatus(candidate)
		plan.Method = &method
		details, err := json.Marshal(candidate.Details)
		if err != nil {
			return model.ItemOutcome{},
				fmt.Errorf("encode item outcome evidence: %w", err)
		}
		plan.Details = details
		id, candidateState := candidate.ID, candidate.State
		if state == "SOURCE_CHANGED" {
			candidateState = "SOURCE_CHANGED"
		}
		plan.CandidateID, plan.CandidateState = &id, &candidateState
	}
	event, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"phase":         "INSTALLING",
		"result":        state,
	})
	if err != nil {
		return model.ItemOutcome{},
			fmt.Errorf("encode item outcome event: %w", err)
	}
	plan.Event = event
	return plan, nil
}
