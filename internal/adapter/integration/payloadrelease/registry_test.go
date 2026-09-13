package payloadrelease

import "testing"

func TestOwnershipRegistryExactlyClassifiesBlobRegistry(t *testing.T) {
	t.Parallel()
	if err := ValidateOwnershipRegistry(); err != nil {
		t.Fatal(err)
	}
}
