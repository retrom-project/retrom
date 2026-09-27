package storageanalysis

import (
	"errors"
	"math"
	"testing"
)

func TestClassifyUsesOnlyTheFileOwnerAndRetirement(t *testing.T) {
	for usage, category := range map[Usage]CategoryCode{UsageGame: CategoryGameContent, UsageBIOS: CategoryBIOS, UsageSaves: CategorySaves, UsageMedia: CategoryMedia, UsageWorkflow: CategoryWorkflow} {
		got, err := classify(true, usage)
		if err != nil || got != category {
			t.Fatalf("owner %v: %s %v", usage, got, err)
		}
		got, err = classify(false, usage)
		if err != nil || got != CategoryPendingDelete {
			t.Fatalf("retired %v: %s %v", usage, got, err)
		}
	}
	if _, err := classify(true, 0); err == nil {
		t.Fatal("unknown owner accepted")
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
