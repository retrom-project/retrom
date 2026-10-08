package saves

import (
	"context"
	"strings"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
)

func (s *Service) availabilityBatch(ctx context.Context, items []model.Save) error {
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !seen[item.Game.ID] {
			seen[item.Game.ID] = true
			ids = append(ids, item.Game.ID)
		}
	}
	facts, err := s.Repository.RuntimeFacts(ctx, ids)
	if err != nil {
		return wrap(err)
	}
	inputs, indices, games := s.identityInputs(items, facts)

	var identities []struct {
		CoreID          string   `json:"coreId"`
		ProviderID      string   `json:"providerId"`
		TargetID        string   `json:"targetId"`
		CoreFingerprint string   `json:"coreFingerprint"`
		ROMHash         string   `json:"romHash"`
		ReadFormats     []string `json:"readFormats"`
		Error           string   `json:"error"`
	}
	if err = s.Runs.Runtime.Call(ctx, "batch-identity", map[string]any{"items": inputs}, &identities); err != nil {
		return wrap(err)
	}
	restoreInputs := make([]map[string]any, 0, len(items))
	for _, save := range items {
		var current *model.Extinfo
		formats := []string{}
		key := save.Game.ID + "/" + save.Extinfo.CoreID
		if index, exists := indices[key]; exists && index < len(identities) && identities[index].Error == "" {
			value := identities[index]
			current = &model.Extinfo{
				CoreID:          value.CoreID,
				ProviderID:      value.ProviderID,
				TargetID:        value.TargetID,
				CoreFingerprint: value.CoreFingerprint,
				ROMHash:         value.ROMHash,
			}
			formats = value.ReadFormats
		}
		restoreInputs = append(restoreInputs,
			map[string]any{
				"gameAvailable": games[save.Game.ID],
				"saveAvailable": true,
				"current":       current,
				"context":       save.Extinfo,
				"readFormats":   formats,
			})
	}
	var results []struct {
		Restorable bool    `json:"restorable"`
		Reason     *string `json:"reason"`
	}
	if err = s.Runs.Runtime.Call(ctx, "batch-restorable", map[string]any{"items": restoreInputs}, &results); err != nil {
		return wrap(err)
	}
	if len(results) != len(items) {
		return model.ErrUnavailable
	}
	for i, result := range results {
		items[i].Restorable = result.Restorable
		items[i].RestoreReason = ""
		if result.Reason != nil {
			items[i].RestoreReason = strings.ToLower(*result.Reason)
		}
	}
	return nil
}

func (s *Service) identityInputs(items []model.Save, facts []persistence.RuntimeFacts) ([]map[string]any,
	map[string]int, map[string]bool,
) {
	inputs := make([]map[string]any, 0, len(items))
	indices := make(map[string]int, len(items))
	games := make(map[string]bool, len(facts))
	for _, fact := range facts {
		games[fact.GameID] = true
		for _, save := range items {
			key := fact.GameID + "/" + save.Extinfo.CoreID
			if save.Game.ID != fact.GameID {
				continue
			}
			if _, exists := indices[key]; exists {
				continue
			}
			indices[key] = len(inputs)
			inputs = append(inputs,
				map[string]any{
					"directory": map[string]any{
						"platformId":     fact.Directory.PlatformID,
						"defaultCoreId":  fact.Directory.DefaultCoreID,
						"allowedCoreIds": fact.Directory.CoreIDs,
					},
					"config":       fact.Config,
					"files":        runtimeclient.Files(fact.Files),
					"coreId":       save.Extinfo.CoreID,
					"fingerprints": s.Runs.Runtime.Fingerprints,
				})
		}
	}
	return inputs, indices, games
}
