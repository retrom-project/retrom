package dependencies

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/runtimecatalog"
)

type runtimeTarget struct {
	providerID        string
	targetID          string
	contractSHA256    string
	gameCompatibility string
}

func (set *Set) targetForCore(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, coreID string,
) (runtimeTarget, error) {
	var selected *runtimecatalog.Binding
	for index := range set.RuntimeCatalog.Bindings {
		binding := &set.RuntimeCatalog.Bindings[index]
		if binding.CoreID != coreID {
			continue
		}
		if selected == nil {
			selected = binding
			continue
		}
		if selected.ProviderID != binding.ProviderID || selected.TargetID != binding.TargetID {
			return runtimeTarget{}, fmt.Errorf("%w: ambiguous runtime target for core %s", ErrInvalid, coreID)
		}
	}
	if selected == nil {
		return runtimeTarget{}, fmt.Errorf("%w: runtime target missing for core %s", ErrInvalid, coreID)
	}
	result := runtimeTarget{providerID: selected.ProviderID, targetID: selected.TargetID}
	if err := query.QueryRowContext(ctx, `
SELECT target_contract_sha256,game_compatibility_line
FROM runtime_targets
WHERE provider_id=? AND target_id=?
`, result.providerID, result.targetID).Scan(&result.contractSHA256, &result.gameCompatibility); err != nil {
		return runtimeTarget{}, fmt.Errorf("resolve runtime target for core %s: %w", coreID, err)
	}
	return result, nil
}
