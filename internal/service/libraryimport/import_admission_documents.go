package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
)

type admissionConfigDocument struct {
	SchemaVersion                 int                      `json:"schemaVersion"`
	BindingState                  string                   `json:"bindingState"`
	ContentMode                   string                   `json:"contentMode"`
	PlatformInstanceID            string                   `json:"platformInstanceId"`
	PlatformInstanceVersion       int64                    `json:"platformInstanceVersion"`
	PlatformID                    string                   `json:"platformId"`
	DefaultCoreID                 string                   `json:"defaultCoreId"`
	ResolvedCoreID                *string                  `json:"resolvedCoreId"`
	ProviderID                    string                   `json:"providerId"`
	TargetID                      string                   `json:"targetId"`
	MetadataProviderConfigVersion int                      `json:"metadataProviderConfigVersion"`
	Tags                          []taggingmodel.Reference `json:"tags"`
}
type admissionInputDocument struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Kind          string              `json:"kind"`
	Scope         admissionInputScope `json:"scope"`
	ExecutionID   string              `json:"executionId"`
	Inputs        admissionInputFacts `json:"inputs"`
}
type admissionInputScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type admissionInputFacts struct {
	UploadID       string `json:"uploadSessionId"`
	UploadVersion  int64  `json:"uploadVersion"`
	ManifestDigest string `json:"manifestDigest"`
	ConfigDigest   string `json:"importConfigSnapshotDigest"`
}

func admissionDocuments(change model.ImportAdmissionChange, tags []taggingmodel.Reference) (
	model.ImportAdmissionDocuments,
	error,
) {
	var documents model.ImportAdmissionDocuments
	var err error
	documents.RequestJSON, documents.RequestDigest, err = encodeAdmissionDocument(model.QueuedImportRequest{
		SchemaVersion: 1,
		Request:       change.Request,
		Tags:          tags,
	})
	if err != nil {
		return model.ImportAdmissionDocuments{}, err
	}
	documents.TargetJSON, documents.TargetDigest, err = encodeAdmissionDocument(change.TargetSnapshot)
	if err != nil {
		return model.ImportAdmissionDocuments{}, err
	}
	target := change.Target
	documents.ConfigJSON, documents.ConfigDigest, err = encodeAdmissionDocument(admissionConfigDocument{
		SchemaVersion: 3, BindingState: "PENDING", ContentMode: change.ContentMode,
		PlatformInstanceID:      change.Request.TargetPlatformInstanceID,
		PlatformInstanceVersion: target.Version, PlatformID: target.PlatformID, DefaultCoreID: target.DefaultCoreID,
		ProviderID: target.ProviderID, TargetID: target.TargetID, MetadataProviderConfigVersion: 1, Tags: tags,
	})
	if err != nil {
		return model.ImportAdmissionDocuments{}, err
	}
	documents.InputJSON, documents.InputDigest, err = encodeAdmissionDocument(admissionInputDocument{
		SchemaVersion: 1, Kind: "IMPORT_GROUP", Scope: admissionInputScope{Type: "IMPORT_GROUP", ID: change.ImportID},
		ExecutionID: change.ExecutionID,
		Inputs: admissionInputFacts{
			UploadID: change.Request.UploadID, UploadVersion: change.Upload.Version,
			ManifestDigest: change.Upload.ManifestDigest, ConfigDigest: documents.RequestDigest,
		},
	})
	if err != nil {
		return model.ImportAdmissionDocuments{}, err
	}
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00IMPORT_GROUP\x00" + change.ImportID))
	documents.DedupeKey = hex.EncodeToString(dedupe[:])
	return documents, nil
}

func encodeAdmissionDocument[T any](value T) (string, string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("encode import admission snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:]), nil
}

// MatchesImportDocumentDigest checks the same canonical JSON used at admission.
func MatchesImportDocumentDigest(document, digest string) bool {
	_, actual, err := encodeAdmissionDocument(json.RawMessage(document))
	return err == nil && actual == digest
}
