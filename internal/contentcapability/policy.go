package contentcapability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

type MultiDiscPolicy struct {
	MultiDiscLimits
	Delivery string `json:"delivery"`
}

type Policy struct {
	SupportedContentKinds []string         `json:"supportedContentKinds"`
	MultiDisc             *MultiDiscPolicy `json:"multiDisc"`
}

func NewPolicy(kinds ...string) Policy {
	policy := Policy{SupportedContentKinds: slices.Clone(kinds)}
	slices.Sort(policy.SupportedContentKinds)
	policy.SupportedContentKinds = slices.Compact(policy.SupportedContentKinds)
	if slices.Contains(policy.SupportedContentKinds, ModeMultiDisc) {
		policy.MultiDisc = &MultiDiscPolicy{
			MultiDiscLimits: MultiDiscLimits{MaxDiscs: MaximumMultiDiscCount, MaxTotalBytes: MaximumMultiDiscBytes},
			Delivery:        DeliveryEagerExternal,
		}
	}
	return policy
}

func (policy Policy) Supports(contentKind string) bool {
	return slices.Contains(policy.SupportedContentKinds, contentKind)
}

func (policy Policy) Digest() string {
	if len(policy.SupportedContentKinds) == 0 {
		return ""
	}
	canonical := policy
	canonical.SupportedContentKinds = slices.Clone(policy.SupportedContentKinds)
	slices.Sort(canonical.SupportedContentKinds)
	canonical.SupportedContentKinds = slices.Compact(canonical.SupportedContentKinds)
	encoded, _ := json.Marshal(canonical)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// DigestFor binds validation to the selected content policy. Adding an
// unrelated accepted kind must not invalidate a pending review or saved game.
func (policy Policy) DigestFor(contentKind string) string {
	if !policy.Supports(contentKind) {
		return ""
	}
	var multiDisc *MultiDiscPolicy
	if contentKind == ModeMultiDisc {
		multiDisc = policy.MultiDisc
	}
	encoded, _ := json.Marshal(struct {
		ContentKind string           `json:"contentKind"`
		MultiDisc   *MultiDiscPolicy `json:"multiDisc"`
	}{ContentKind: contentKind, MultiDisc: multiDisc})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
