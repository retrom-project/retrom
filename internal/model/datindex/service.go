package datindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Records is supplied by the caller's transaction so requirements publish with the DAT index and activation.
type Records interface {
	Definition(context.Context, string) (Definition, error)
	MachineNames(context.Context, string) ([]string, error)
	RequiredEntries(context.Context, string, string) ([]Entry, error)
	UpsertRequirement(context.Context, Requirement) error
	DisableStale(context.Context, Retirement) error
}

type (
	Definition struct{ CoreID, ProviderID, TargetID, SHA256 string }
	Entry      struct {
		BIOSName  *string `json:"biosName"`
		CRC32     *string `json:"crc32"`
		MergeName *string `json:"mergeName"`
		Name      string  `json:"name"`
		SHA1      *string `json:"sha1"`
		SizeBytes int64   `json:"sizeBytes"`
		Status    string  `json:"status"`
	}
)

type Requirement struct {
	ID, CoreID, ProviderID, TargetID, Machine, LogicalName, Digest, SourceURL, VersionID string
	AtMS                                                                                 int64
}
type Retirement struct {
	ProviderID, TargetID, CurrentVersionID string
	AtMS                                   int64
}

func SyncRequirements(ctx context.Context, records Records, datID string, now time.Time) error {
	definition, err := records.Definition(ctx, datID)
	if err != nil {
		return fmt.Errorf("datindex/read definition: %w", err)
	}
	machines, err := records.MachineNames(ctx, datID)
	if err != nil {
		return fmt.Errorf("datindex/read machines: %w", err)
	}
	for _, machine := range machines {
		entries, err := records.RequiredEntries(ctx, datID, machine)
		if err != nil {
			return fmt.Errorf("datindex/read requirement entries: %w", err)
		}
		requirement, err := buildRequirement(definition, datID, machine, entries, now.UnixMilli())
		if err != nil {
			return err
		}
		if err := records.UpsertRequirement(ctx, requirement); err != nil {
			return fmt.Errorf("datindex/sync requirement: %w", err)
		}
	}
	if err := records.DisableStale(ctx, Retirement{
		ProviderID: definition.ProviderID, TargetID: definition.TargetID,
		CurrentVersionID: datID, AtMS: now.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("datindex/retire requirements: %w", err)
	}
	return nil
}

func buildRequirement(definition Definition, datID, machine string, entries []Entry, now int64) (Requirement, error) {
	canonical, err := json.Marshal(map[string]any{
		"datSha256": definition.SHA256, "entries": entries, "machineName": machine, "schemaVersion": 1,
	})
	if err != nil {
		return Requirement{}, fmt.Errorf("datindex/encode requirement: %w", err)
	}
	digest := sha256.Sum256(canonical)
	logicalName := machine + ".zip"
	id := uuid.NewSHA1(
		uuid.NameSpaceURL,
		[]byte(
			"retrom:bios:"+definition.ProviderID+":"+definition.TargetID+":"+logicalName,
		),
	).String()
	return Requirement{
		ID: id, CoreID: definition.CoreID, ProviderID: definition.ProviderID, TargetID: definition.TargetID,
		Machine: machine, LogicalName: logicalName, Digest: hex.EncodeToString(digest[:]),
		SourceURL: fmt.Sprintf("retrom:dat:%s#%s", datID, machine), VersionID: datID, AtMS: now,
	}, nil
}

func BuildRequirement(definition Definition, datID, machine string, entries []Entry, now int64) (Requirement, error) {
	return buildRequirement(definition, datID, machine, entries, now)
}
