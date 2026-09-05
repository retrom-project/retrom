package contentcapability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// BindingPolicySQL is a scalar column for queries whose selected Host binding
// is aliased as binding. Scan it into Policy in the same statement/transaction
// as the source, target and dependency facts. Only relational kind names cross
// the SQL boundary; delivery and limits are constructed once in Go.
const BindingPolicySQL = `(SELECT group_concat(content_kind, ',') FROM (
 SELECT content_kind FROM runtime_binding_content_kinds
 WHERE binding_id=binding.binding_id ORDER BY content_kind
))`

var errInvalidKindColumn = errors.New("contentcapability: invalid kind column")

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

// Scan implements sql.Scanner, including absent optional bindings. It does not
// issue another query, decode JSON or retain a previous row's capabilities.
func (policy *Policy) Scan(value any) error {
	*policy = Policy{}
	if value == nil {
		return nil
	}
	var names string
	switch value := value.(type) {
	case string:
		names = value
	case []byte:
		names = string(value)
	default:
		return fmt.Errorf("%w: %T", errInvalidKindColumn, value)
	}
	if names != "" {
		*policy = NewPolicy(strings.Split(names, ",")...)
	}
	return nil
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
