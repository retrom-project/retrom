package launch

import (
	"context"
	"errors"
	"testing"
)

func TestProductProviderChecksRunBeforeWriter(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ProductCreator, *productTestRepository, *previewTestProvider)
	}{
		{"missing provider", func(service *ProductCreator, _ *productTestRepository, _ *previewTestProvider) {
			service.provider = nil
		}},
		{"absent target", func(_ *ProductCreator, _ *productTestRepository, provider *previewTestProvider) {
			provider.absent = true
		}},
		{"changed bundle", func(_ *ProductCreator, repository *productTestRepository, _ *previewTestProvider) {
			repository.before.Source.BundleSHA256 = "changed"
		}},
		{"thread capabilities", func(_ *ProductCreator, _ *productTestRepository, provider *previewTestProvider) {
			provider.target.Capabilities.RequiresThreads = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repository, provider, command := productFixture(t)
			test.change(service, repository, provider)
			result, err := service.Create(t.Context(), command)
			if !errors.Is(err, ErrBlocked) || result.Created.LaunchID != "" || repository.transactions != 0 {
				t.Fatalf("launch=%q error=%v transactions=%d", result.Created.LaunchID, err, repository.transactions)
			}
		})
	}
}

func TestProductCapabilityFailureCannotOpenWriter(t *testing.T) {
	service, repository, _, command := productFixture(t)
	service.environment.SignCapability = func(string) (string, []byte, error) { return "", nil, context.Canceled }
	result, err := service.Create(t.Context(), command)
	if !errors.Is(err, context.Canceled) || result.Created.LaunchID != "" || repository.transactions != 0 {
		t.Fatalf("signing launch=%q error=%v transactions=%d", result.Created.LaunchID, err, repository.transactions)
	}
}

func TestProductValidationRejectsMalformedVariantIdentity(t *testing.T) {
	service, repository, _, command := productFixture(t)
	repository.before.Source.VariantID, repository.before.Source.VariantStatus = "", ""
	repository.current = cloneProductSnapshot(t, repository.before)
	service.environment.NewID = func() (string, error) { return "00000000-0000-0000-0000-000000000000", nil }
	result, err := service.Create(t.Context(), command)
	if !errors.Is(err, ErrBlocked) || result.Created.JobID != "" || len(repository.variants) != 0 {
		t.Fatalf("variant identity job=%q error=%v variants=%d", result.Created.JobID, err, len(repository.variants))
	}
}
