package storageanalysis

import (
	"errors"
	"math"
	"testing"
)

func TestClassifyUsesDurablePrecedenceAndSharedFallback(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		protected bool
		flags     Usage
		want      CategoryCode
	}{
		"unreferenced ignores flags": {false, UsageGame, CategoryUnreferenced},
		"durable wins over workflow": {true, UsageGame | UsageWorkflow, CategoryGameContent},
		"durable wins over runtime":  {true, UsageBIOS | UsageRuntime, CategoryBIOS},
		"shared durable":             {true, UsageSaves | UsageMedia, CategorySharedDurable},
		"workflow before runtime":    {true, UsageWorkflow | UsageRuntime, CategoryWorkflow},
		"runtime":                    {true, UsageRuntime, CategoryRuntimeSnapshot},
		"other protected":            {true, 0, CategoryOtherReferenced},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := classify(test.protected, test.flags); got != test.want {
				t.Fatalf("classify() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestAddCheckedRejectsOverflow(t *testing.T) {
	t.Parallel()
	if _, err := addChecked(math.MaxInt64, 1); !errors.Is(err, errIntegerOverflow) {
		t.Fatalf("positive overflow error = %v", err)
	}
	if _, err := addChecked(math.MinInt64, -1); !errors.Is(err, errIntegerOverflow) {
		t.Fatalf("negative overflow error = %v", err)
	}
}
