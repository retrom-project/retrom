package payloadrelease

import (
	"errors"
	"fmt"
	"sort"

	"retrom/internal/blobregistry"
)

var errOwnershipRegistryMismatch = errors.New("PAYLOAD_OWNERSHIP_REGISTRY_MISMATCH")

type Ownership string

const (
	GameOwned                 Ownership = "GAME_OWNED"
	GameRuntimeOwned          Ownership = "GAME_RUNTIME_OWNED"
	ImportItemOwned           Ownership = "IMPORT_ITEM_OWNED"
	PegasusItemOwned          Ownership = "PEGASUS_ITEM_OWNED"
	EmulationStationItemOwned Ownership = "EMULATIONSTATION_ITEM_OWNED"
	ScrapeRunOwned            Ownership = "SCRAPE_RUN_OWNED"
	UploadOwned               Ownership = "UPLOAD_OWNED"
	GlobalTTL                 Ownership = "GLOBAL_TTL"
	GlobalDurable             Ownership = "GLOBAL_DURABLE"
	ArchiveOwnership          Ownership = "ARCHIVE_OWNERSHIP"
	Bookkeeping               Ownership = "BOOKKEEPING"
)

type OwnershipEdge struct {
	Table     string
	Column    string
	Ownership Ownership
}

// ownershipRegistry is the lifecycle authority for every Blob edge. It is
// deliberately code, rather than a prose mirror, so schema tests and release
// assertions consume the same inventory.
var ownershipRegistry = []OwnershipEdge{
	{"archive_entries", "archive_blob_id", ArchiveOwnership},
	{"archive_entries", "materialized_blob_id", ArchiveOwnership},
	{"bios_installations", "blob_id", GlobalDurable},
	{"blob_gc_candidates", "blob_id", Bookkeeping},
	{"content_hash_evidence", "archive_blob_id", ScrapeRunOwned},
	{"content_hash_evidence", "blob_id", ScrapeRunOwned},
	{"game_assets", "blob_id", GameOwned},
	{"game_files", "blob_id", GameOwned},
	{"game_files", "source_archive_blob_id", GameOwned},
	{"import_item_source_files", "blob_id", ImportItemOwned},
	{"import_item_source_files", "source_archive_blob_id", ImportItemOwned},
	{"import_item_source_snapshot_files", "blob_id", ImportItemOwned},
	{"import_item_source_snapshot_files", "source_archive_blob_id", ImportItemOwned},
	{"import_item_multidisc_entries", "blob_id", ImportItemOwned},
	{"import_item_validation_files", "blob_id", ImportItemOwned},
	{"review_arcade_parent_attachments", "accepted_blob_id", ImportItemOwned},
	{"launch_content_files", "blob_id", GameRuntimeOwned},
	{"launch_external_files", "blob_id", GameRuntimeOwned},
	{"metadata_provider_responses", "raw_response_blob_id", GlobalTTL},
	{"emulationstation_import_item_assets", "blob_id", EmulationStationItemOwned},
	{"emulationstation_import_item_files", "blob_id", EmulationStationItemOwned},
	{"emulationstation_import_item_files", "source_archive_blob_id", EmulationStationItemOwned},
	{"pegasus_import_item_assets", "blob_id", PegasusItemOwned},
	{"pegasus_import_item_files", "blob_id", PegasusItemOwned},
	{"pegasus_import_item_files", "source_archive_blob_id", PegasusItemOwned},
	{"review_uploaded_assets", "blob_id", ImportItemOwned},
	{"review_preview_sessions", "content_blob_id", ImportItemOwned},
	{"review_preview_sessions", "checkpoint_payload_blob_id", ImportItemOwned},
	{"review_preview_sessions", "restore_payload_blob_id", ImportItemOwned},
	{"review_preview_files", "blob_id", ImportItemOwned},
	{"review_runtime_screenshots", "blob_id", ImportItemOwned},
	{"runtime_asset_pack_files", "blob_id", GlobalDurable},
	{"runtime_asset_pack_installations", "bundle_blob_id", GlobalDurable},
	{"save_states", "payload_blob_id", GameRuntimeOwned},
	{"save_states", "screenshot_blob_id", GameRuntimeOwned},
	{"scrape_candidate_assets", "blob_id", ScrapeRunOwned},
	{"upload_files", "final_blob_id", UploadOwned},
	{"variant_files", "blob_id", GameOwned},
}

func OwnershipRegistry() []OwnershipEdge {
	result := make([]OwnershipEdge, len(ownershipRegistry))
	copy(result, ownershipRegistry)
	return result
}

func ValidateOwnershipRegistry() error {
	edges, err := blobregistry.Load()
	if err != nil {
		return fmt.Errorf("payloadrelease/registry: %w", err)
	}
	want := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		want[edge.Table+"."+edge.Column] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ownershipRegistry))
	var problems []string
	for _, edge := range ownershipRegistry {
		key := edge.Table + "." + edge.Column
		if edge.Ownership == "" {
			problems = append(problems, "unclassified:"+key)
		}
		if _, duplicate := seen[key]; duplicate {
			problems = append(problems, "duplicate:"+key)
		}
		seen[key] = struct{}{}
		if _, exists := want[key]; !exists {
			problems = append(problems, "ownership-only:"+key)
		}
	}
	for key := range want {
		if _, exists := seen[key]; !exists {
			problems = append(problems, "blob-registry-only:"+key)
		}
	}
	sort.Strings(problems)
	if len(problems) != 0 {
		return fmt.Errorf("%w: %v", errOwnershipRegistryMismatch, problems)
	}
	return nil
}
