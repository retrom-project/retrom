// Package requirements evaluates parsed content facts against declared target
// requirements. Neither the parser nor the policy owns import or UI state.
package requirements

import (
	"retrom/internal/content/diagnostic"
	"retrom/internal/format/nintendo3ds"
)

const (
	Decrypted3DS     = "DECRYPTED_NCSD_NCCH"
	FlycastCartridge = "FLYCAST_CARTRIDGE"
)

type (
	Policy struct {
		Kind         string          `json:"kind"`
		Platform     string          `json:"platform,omitempty"`
		Catalog      *Asset          `json:"catalog,omitempty"`
		Core         *Asset          `json:"core,omitempty"`
		CatalogFacts *FlycastCatalog `json:"-"`
		CatalogJSON  []byte          `json:"-"`
	}
	Facts struct {
		Archive     []ArchiveMember    `json:"archive,omitempty"`
		Nintendo3DS *nintendo3ds.Facts `json:"nintendo3ds,omitempty"`
	}
)

func (policy Policy) Valid() bool {
	switch policy.Kind {
	case Decrypted3DS:
		return policy.Platform == "" && policy.Catalog == nil && policy.Core == nil
	case FlycastCartridge:
		return validPlatform(policy.Platform) && policy.Catalog != nil && policy.Catalog.Valid() &&
			policy.Core != nil && policy.Core.Valid()
	default:
		return false
	}
}

func (policy *Policy) Evaluate(facts Facts, name string) *diagnostic.Rejection {
	if policy == nil {
		return nil
	}
	if policy.Kind == FlycastCartridge {
		return policy.evaluateCartridge(facts, name)
	}
	if policy.Kind == Decrypted3DS {
		parsed := facts.Nintendo3DS
		if parsed == nil || len(parsed.Partitions) == 0 || parsed.Partitions[0].Index != 0 ||
			!parsed.Partitions[0].Executable {
			return &diagnostic.Rejection{Code: "THREEDS_CONTAINER_INVALID", RelativePath: name}
		}
		if parsed.Partitions[0].Encrypted {
			return &diagnostic.Rejection{Code: "THREEDS_ENCRYPTED_CONTENT", RelativePath: name}
		}
	}
	return nil
}
