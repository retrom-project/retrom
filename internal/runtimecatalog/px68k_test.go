package runtimecatalog

import "testing"

func TestPX68KUsesBIOSAwareSingleDiskDelivery(t *testing.T) {
	strategy, ok := Strategy("PX68K_DISK")
	if !ok || strategy.Delivery != "EMULATORJS_CONTENT" || strategy.Options != OptionsNone {
		t.Fatalf("PX68K strategy: %+v", strategy)
	}
	if len(strategy.ContentKinds) != 1 || strategy.ContentKinds[0] != "SINGLE_FILE" {
		t.Fatal(strategy.ContentKinds)
	}
}
