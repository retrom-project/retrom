// Package contentquery maps relational content capability facts into domain policies.
package contentquery

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	contentcapability "retrom/internal/content/capability"
	"retrom/internal/content/requirements"
)

// BindingPolicySQL projects kinds and Provider-owned input facts in the same
// snapshot as the selected Host binding. JSONB also gives equality fences
// semantic comparison with BindPolicy, independent of JSON key order or spacing.
const BindingPolicySQL = `jsonb_build_object(
 'kinds',COALESCE((SELECT jsonb_agg(content_kind ORDER BY content_kind)
  FROM runtime_binding_content_kinds WHERE binding_id=binding.binding_id),'[]'::jsonb),
 'limits',COALESCE((SELECT jsonb_object_agg(role,max_file_bytes ORDER BY role)
  FROM runtime_target_input_limits WHERE provider_id=binding.provider_id
  AND target_id=binding.target_id AND max_file_bytes IS NOT NULL),'{}'::jsonb),
 'requirements',(SELECT manifest_fragment_json::jsonb -> 'contentRequirements' FROM runtime_targets
  WHERE provider_id=binding.provider_id AND target_id=binding.target_id))`

var errInvalidKindColumn = errors.New("contentcapability: invalid kind column")

// ScanPolicy maps a scalar projection into the supplied domain value.
func ScanPolicy(policy *contentcapability.Policy) sql.Scanner { return policyColumn{policy: policy} }

type policyColumn struct{ policy *contentcapability.Policy }

// Scan implements sql.Scanner, including absent optional bindings. It does not
// issue another query or retain a previous row's capabilities.
func (column policyColumn) Scan(value any) error {
	policy := column.policy
	*policy = contentcapability.Policy{}
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
	if names == "" {
		return nil
	}
	var projection struct {
		Kinds        []string             `json:"kinds"`
		Limits       map[string]int64     `json:"limits"`
		Requirements *requirements.Policy `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(names), &projection); err != nil {
		return fmt.Errorf("%w: %w", errInvalidKindColumn, err)
	}
	for _, maximum := range projection.Limits {
		if maximum < 1 || maximum > 9007199254740991 {
			return errInvalidKindColumn
		}
	}
	*policy = contentcapability.NewPolicy(projection.Kinds...)
	policy.InputMaxFileBytes = projection.Limits
	policy.Requirements = projection.Requirements
	if policy.Requirements != nil && !policy.Requirements.Valid() {
		return errInvalidKindColumn
	}
	return nil
}

// BindPolicy encodes the same ordered relational facts as BindingPolicySQL for
// transactional equality fences. Derived domain rules do not belong in SQL.
func BindPolicy(policy contentcapability.Policy) driver.Valuer { return policyColumn{policy: &policy} }

func (column policyColumn) Value() (driver.Value, error) {
	limits := make(map[string]int64, len(column.policy.InputMaxFileBytes))
	for role, maximum := range column.policy.InputMaxFileBytes {
		limits[role] = maximum
	}
	value, err := json.Marshal(struct {
		Kinds        []string             `json:"kinds"`
		Limits       map[string]int64     `json:"limits"`
		Requirements *requirements.Policy `json:"requirements"`
	}{
		contentcapability.NewPolicy(column.policy.SupportedContentKinds...).SupportedContentKinds,
		limits, column.policy.Requirements,
	})
	if err != nil {
		return nil, fmt.Errorf("encode content policy facts: %w", err)
	}
	return string(value), nil
}
