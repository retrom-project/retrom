package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/model/tagging"
)

// AdmissionConfigDocument is the canonical JSON structure for import config snapshots.
type AdmissionConfigDocument struct {
	SchemaVersion                 int                 `json:"schemaVersion"`
	BindingState                  string              `json:"bindingState"`
	ContentMode                   string              `json:"contentMode"`
	PlatformInstanceID            string              `json:"platformInstanceId"`
	PlatformInstanceVersion       int64               `json:"platformInstanceVersion"`
	PlatformID                    string              `json:"platformId"`
	DefaultCoreID                 string              `json:"defaultCoreId"`
	ResolvedCoreID                *string             `json:"resolvedCoreId"`
	ProviderID                    string              `json:"providerId"`
	TargetID                      string              `json:"targetId"`
	MetadataProviderConfigVersion int                 `json:"metadataProviderConfigVersion"`
	Tags                          []tagging.Reference `json:"tags"`
}

// AdmissionInputDocument is the canonical JSON structure for import input snapshots.
type AdmissionInputDocument struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Kind          string              `json:"kind"`
	Scope         AdmissionInputScope `json:"scope"`
	ExecutionID   string              `json:"executionId"`
	Inputs        AdmissionInputFacts `json:"inputs"`
}

// AdmissionInputScope identifies the scope of an import input document.
type AdmissionInputScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AdmissionInputFacts are the verifiable input facts recorded at admission.
type AdmissionInputFacts struct {
	UploadID       string `json:"uploadSessionId"`
	UploadVersion  int64  `json:"uploadVersion"`
	ManifestDigest string `json:"manifestDigest"`
	ConfigDigest   string `json:"importConfigSnapshotDigest"`
}

// BuildAdmissionDocuments constructs the canonical JSON documents and their
// SHA-256 digests for an import admission. This is a pure function.
func BuildAdmissionDocuments(
	change ImportAdmissionChange,
	tags []tagging.Reference,
) (ImportAdmissionDocuments, error) {
	var documents ImportAdmissionDocuments
	var err error
	documents.RequestJSON, documents.RequestDigest, err = EncodeAdmissionDocument(
		QueuedImportRequest{SchemaVersion: 1, Request: change.Request, Tags: tags},
	)
	if err != nil {
		return ImportAdmissionDocuments{}, err
	}
	documents.TargetJSON, documents.TargetDigest, err = EncodeAdmissionDocument(change.TargetSnapshot)
	if err != nil {
		return ImportAdmissionDocuments{}, err
	}
	target := change.Target
	documents.ConfigJSON, documents.ConfigDigest, err = EncodeAdmissionDocument(AdmissionConfigDocument{
		SchemaVersion: 3, BindingState: "PENDING", ContentMode: change.ContentMode,
		PlatformInstanceID:      change.Request.TargetPlatformInstanceID,
		PlatformInstanceVersion: target.Version, PlatformID: target.PlatformID, DefaultCoreID: target.DefaultCoreID,
		ProviderID: target.ProviderID, TargetID: target.TargetID, MetadataProviderConfigVersion: 1, Tags: tags,
	})
	if err != nil {
		return ImportAdmissionDocuments{}, err
	}
	documents.InputJSON, documents.InputDigest, err = EncodeAdmissionDocument(AdmissionInputDocument{
		SchemaVersion: 1, Kind: "IMPORT_GROUP", Scope: AdmissionInputScope{Type: "IMPORT_GROUP", ID: change.ImportID},
		ExecutionID: change.ExecutionID,
		Inputs: AdmissionInputFacts{
			UploadID: change.Request.UploadID, UploadVersion: change.Upload.Version,
			ManifestDigest: change.Upload.ManifestDigest, ConfigDigest: documents.RequestDigest,
		},
	})
	if err != nil {
		return ImportAdmissionDocuments{}, err
	}
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00IMPORT_GROUP\x00" + change.ImportID))
	documents.DedupeKey = hex.EncodeToString(dedupe[:])
	return documents, nil
}

// EncodeAdmissionDocument marshals a value to canonical JSON and returns both
// the JSON string and its SHA-256 hex digest.
func EncodeAdmissionDocument[T any](value T) (string, string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("encode import admission snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:]), nil
}

// MatchesAdmissionDocumentDigest checks the same canonical JSON used at admission.
func MatchesAdmissionDocumentDigest(document, digest string) bool {
	_, actual, err := EncodeAdmissionDocument(json.RawMessage(document))
	return err == nil && actual == digest
}
