package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/content/contentcapability"
	contentvalidation "retrom/internal/capability/content/corevalidation"
	corevalidationmodel "retrom/internal/model/corevalidation"
	application "retrom/internal/model/libraryimport"
)

type selectedValidationReaderStub struct {
	inputs       application.ReviewValidationRefreshInputs
	selected     application.ReviewValidationRefreshRecord
	selectedErr  error
	selectedSeen string
}

func (stub *selectedValidationReaderStub) Inputs(
	context.Context, string, string,
) (application.ReviewValidationRefreshInputs, error) {
	return stub.inputs, nil
}

func (stub *selectedValidationReaderStub) Selected(
	_ context.Context, _, validationID string,
) (application.ReviewValidationRefreshRecord, bool, error) {
	stub.selectedSeen = validationID
	if stub.selectedErr != nil {
		return application.ReviewValidationRefreshRecord{}, false, stub.selectedErr
	}
	return stub.selected, true, nil
}

func (stub *selectedValidationReaderStub) Exact(
	context.Context, application.ReviewValidationRefreshLookup,
) (application.ReviewValidationRefreshRecord, bool, error) {
	return application.ReviewValidationRefreshRecord{}, false, nil
}

func (stub *selectedValidationReaderStub) Fallback(
	context.Context, application.ReviewValidationRefreshLookup,
) (application.ReviewValidationRefreshRecord, bool, error) {
	return application.ReviewValidationRefreshRecord{}, false, nil
}

func (stub *selectedValidationReaderStub) ContentLogicalName(context.Context, string) (string, error) {
	return "game.gba", nil
}

func (stub *selectedValidationReaderStub) RPGProfile(context.Context, string) (application.RPGReviewProfile, error) {
	return application.RPGReviewProfile{}, errors.New("unexpected RPG profile read")
}

type selectedValidationDependenciesStub struct {
	records []corevalidationmodel.BIOSRecord
}

func (stub selectedValidationDependenciesStub) BIOS(
	context.Context, string, string,
) ([]corevalidationmodel.BIOSRecord, error) {
	return stub.records, nil
}

func (selectedValidationDependenciesStub) ArcadeBIOS(
	context.Context, string, string, string,
) (contentvalidation.BIOSDependency, bool, error) {
	return contentvalidation.BIOSDependency{}, false, nil
}

func TestReviewDraftValidationRejectsStaleExplicitSelection(t *testing.T) {
	t.Parallel()
	reader, dependencies := selectedValidationFixture(t, true)
	resolver := NewReviewDraftValidationResolver(reader, reader, dependencies, func() time.Time { return time.UnixMilli(1) })

	_, err := resolver.ResolveSelected(t.Context(), ReviewDraftSelectedValidationRequest{
		ItemID: "item", TargetPlatformInstanceID: "platform", ValidationID: "validation",
	})
	if !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if reader.selectedSeen != "validation" {
		t.Fatalf("selected validation read = %q", reader.selectedSeen)
	}
}

func TestReviewDraftValidationAcceptsCurrentExplicitSelection(t *testing.T) {
	t.Parallel()
	reader, dependencies := selectedValidationFixture(t, false)
	resolver := NewReviewDraftValidationResolver(reader, reader, dependencies, func() time.Time { return time.UnixMilli(1) })

	plan, err := resolver.ResolveSelected(t.Context(), ReviewDraftSelectedValidationRequest{
		ItemID: "item", TargetPlatformInstanceID: "platform", ValidationID: "validation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedValidationID != "validation" || plan.Create != nil || plan.Copy != nil ||
		plan.SelectedValidationPrepublishDigest != reader.selected.PrepublishInputDigest {
		t.Fatalf("selected plan = %#v", plan)
	}
}

func selectedValidationFixture(
	t *testing.T, missing bool,
) (*selectedValidationReaderStub, selectedValidationDependenciesStub) {
	t.Helper()
	policy := contentcapability.NewPolicy("SINGLE_FILE")
	inputs := application.ReviewValidationRefreshInputs{
		DraftID: "draft", EffectiveSnapshotID: "snapshot", EffectiveManifestDigest: "manifest",
		ContentKind: "SINGLE_FILE", PlatformID: "gba", CoreID: "mgba", ProviderID: "provider",
		RuntimeTargetID: "target", PlatformVersion: 1, ContentPolicy: policy,
		DependencyFactsDigest: "current-facts",
	}
	installed := "installed"
	status := "MATCHED"
	currentRecord := corevalidationmodel.BIOSRecord{Dependency: contentvalidation.BIOSDependency{
		BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{
			RequirementID: "bios", RequirementVersion: 1, CatalogDigest: "catalog",
			LogicalName: "bios.bin", RequirementMode: "REQUIRED", DeliveryKind: "BIOS_BUNDLE",
		}, BlobID: &installed, InstallationStatus: &status,
	}}
	selectedRecord := currentRecord
	if missing {
		selectedRecord.Dependency.BlobID = nil
		selectedRecord.Dependency.InstallationStatus = nil
	}
	selectedSnapshot, selectedStatus, selectedCode, err := corevalidationmodel.ResolveBIOSRecords(
		[]corevalidationmodel.BIOSRecord{currentRecord}, "game.gba",
	)
	if err != nil {
		t.Fatal(err)
	}
	selectedJSON, err := selectedSnapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		selectedStatus, selectedCode = "READY", "READY"
	}
	prepublishDigest := application.PrepublishDigest(application.PrepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: inputs.EffectiveSnapshotID,
		SourceManifestDigest: inputs.EffectiveManifestDigest, ContentKind: inputs.ContentKind,
		TargetPlatformInstanceID: "platform", ProviderID: inputs.ProviderID, TargetID: inputs.RuntimeTargetID,
		ContentPolicyDigest: policy.DigestFor(inputs.ContentKind), DependencySnapshot: json.RawMessage(selectedJSON),
		Status: selectedStatus, CompatibilityCode: selectedCode,
	})
	reader := &selectedValidationReaderStub{
		inputs: inputs,
		selected: application.ReviewValidationRefreshRecord{
			ID: "validation", SourceManifestDigest: inputs.EffectiveManifestDigest,
			PrepublishInputDigest: prepublishDigest, Status: selectedStatus,
			CompatibilityCode: selectedCode, DependencySnapshot: string(selectedJSON),
			SourceSnapshotID: inputs.EffectiveSnapshotID, TargetPlatformInstanceID: "platform",
			CoreID: inputs.CoreID, ProviderID: inputs.ProviderID, TargetID: inputs.RuntimeTargetID,
		},
	}
	if missing {
		// Keep the stored validation READY and based on the old installed fact;
		// the resolver must reject it after recomputing the current missing fact.
		oldSnapshot, oldStatus, oldCode, err := corevalidationmodel.ResolveBIOSRecords(
			[]corevalidationmodel.BIOSRecord{currentRecord}, "game.gba",
		)
		if err != nil {
			t.Fatal(err)
		}
		oldJSON, err := oldSnapshot.JSON()
		if err != nil {
			t.Fatal(err)
		}
		reader.selected.DependencySnapshot = string(oldJSON)
		reader.selected.Status, reader.selected.CompatibilityCode = oldStatus, oldCode
		reader.selected.PrepublishInputDigest = application.PrepublishDigest(application.PrepublishDigestInput{
			SchemaVersion: 1, SourceSnapshotID: inputs.EffectiveSnapshotID,
			SourceManifestDigest: inputs.EffectiveManifestDigest, ContentKind: inputs.ContentKind,
			TargetPlatformInstanceID: "platform", ProviderID: inputs.ProviderID, TargetID: inputs.RuntimeTargetID,
			ContentPolicyDigest: policy.DigestFor(inputs.ContentKind), DependencySnapshot: json.RawMessage(oldJSON),
			Status: oldStatus, CompatibilityCode: oldCode,
		})
	}
	return reader, selectedValidationDependenciesStub{records: []corevalidationmodel.BIOSRecord{selectedRecord}}
}
