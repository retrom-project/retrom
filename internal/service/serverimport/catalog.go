package serverimport

import (
	"bytes"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/firmware"
)

type CatalogItem struct {
	State                     string  `json:"-"`
	RequirementID             string  `json:"requirementId"`
	RequirementVersion        int64   `json:"requirementVersion"`
	CoreID                    string  `json:"coreId"`
	CoreName                  string  `json:"coreName"`
	ProviderID                string  `json:"providerId"`
	TargetID                  string  `json:"targetId"`
	SourceKind                string  `json:"sourceKind"`
	ArchiveMembersJSON        *string `json:"archiveMembersJson"`
	LogicalName               string  `json:"logicalName"`
	RequirementMode           string  `json:"requirementMode"`
	ConditionCode             *string `json:"conditionCode"`
	ActivationOptionsJSON     *string `json:"activationOptionsJson"`
	DeliveryKind              string  `json:"deliveryKind"`
	EmulatorPath              *string `json:"emulatorPath"`
	SourceVersion             string  `json:"sourceVersion"`
	CatalogDigest             string  `json:"catalogDigest"`
	DATVersionID              *string `json:"datVersionId"`
	DATMachineName            *string `json:"datMachineName"`
	ExpectedSize              *int64  `json:"expectedSizeBytes"`
	ExpectedMD5               *string `json:"expectedMd5"`
	ExpectedSHA1              *string `json:"expectedSha1"`
	ExpectedSHA256            *string `json:"expectedSha256"`
	ActiveInstallationID      *string `json:"activeInstallationId"`
	ActiveInstallationVersion *int64  `json:"activeInstallationVersion"`
	ActiveBlobSHA256          *string `json:"activeBlobSha256"`
	ActiveStatus              *string `json:"activeStatus"`
	ActiveValidatedVersion    *int64  `json:"activeValidatedRequirementVersion"`
}

func CanonicalCatalogJSON(items []CatalogItem) ([]byte, error) {
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshal catalog snapshot: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode catalog snapshot for canonicalization: %w", err)
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode canonical catalog snapshot: %w", err)
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}

func (item CatalogItem) IsArchive() bool {
	return item.SourceKind == "DAT_MACHINE" || item.ArchiveMembersJSON != nil
}

func (item CatalogItem) ValidateSource(datReady bool) error {
	if item.ArchiveMembersJSON != nil {
		if _, err := firmware.StaticArchiveExpectations(*item.ArchiveMembersJSON); err != nil {
			return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
		}
	}
	if !item.IsArchive() && item.ExpectedMD5 == nil && item.ExpectedSHA1 == nil &&
		item.ExpectedSHA256 == nil && (item.ExpectedSize == nil || *item.ExpectedSize <= 0) {
		return ErrCatalogInvalid
	}
	if item.SourceKind == "DAT_MACHINE" &&
		(item.DATVersionID == nil || !datReady) {
		return ErrCatalogInvalid
	}
	return nil
}

func (item CatalogItem) ApplyArchivePolicy(evaluation *firmware.DATEvaluation) {
	if item.ArchiveMembersJSON != nil {
		firmware.RequireCompleteArchive(evaluation)
	}
}
