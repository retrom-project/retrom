// Package contentquery maps relational content capability facts into domain policies.
package contentquery

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/contentcapability"
)

// BindingPolicySQL is a scalar column for queries whose selected Host binding
// is aliased as binding. Use ScanPolicy in the same statement/transaction
// as the source, target and dependency facts. Only relational kind names cross
// the SQL boundary; delivery and limits are constructed once in Go.
const BindingPolicySQL = `(SELECT group_concat(content_kind, ',') FROM (
 SELECT content_kind FROM runtime_binding_content_kinds
 WHERE binding_id=binding.binding_id ORDER BY content_kind
))`

var errInvalidKindColumn = errors.New("contentcapability: invalid kind column")

// ScanPolicy maps a scalar projection into the supplied domain value.
func ScanPolicy(policy *contentcapability.Policy) sql.Scanner { return policyColumn{policy: policy} }

type policyColumn struct{ policy *contentcapability.Policy }

// Scan implements sql.Scanner, including absent optional bindings. It does not
// issue another query, decode JSON or retain a previous row's capabilities.
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
	if names != "" {
		*policy = contentcapability.NewPolicy(strings.Split(names, ",")...)
	}
	return nil
}
