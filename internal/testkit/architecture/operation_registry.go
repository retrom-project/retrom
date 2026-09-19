package architecture

import "encoding/json"

type OperationRegistry struct {
	SchemaVersion     int               `json:"schemaVersion"`
	Baseline          string            `json:"baseline"`
	InventoryComplete bool              `json:"inventoryComplete"`
	Status            string            `json:"status"`
	Operations        []json.RawMessage `json:"operations"`
	Pending           []string          `json:"pending"`
}

// OperationRecord identifies actual definitions, while schema validation also
// checks that no mandatory fields were omitted from the serialized record.
type OperationRecord struct {
	ID                  string              `json:"operation_id"`
	Owner               string              `json:"owner"`
	RF                  string              `json:"rf_id"`
	Consumers           []string            `json:"consumers"`
	Snapshot            string              `json:"snapshot"`
	Prepare             string              `json:"prepare"`
	Commit              string              `json:"commit"`
	Policies            []string            `json:"policy_symbols"`
	Facts               []OperationFact     `json:"fresh_facts"`
	Guards              []OperationGuard    `json:"guards"`
	Writes              []OperationWrite    `json:"write_set"`
	Idempotency         string              `json:"idempotency"`
	AfterCommit         []OperationEffect   `json:"after_commit"`
	TimeRule            string              `json:"time_rule"`
	FaultPoints         []string            `json:"fault_points"`
	Tests               []string            `json:"test_ids"`
	ContentCoordination ContentCoordination `json:"content_coordination"`
}

type OperationFact struct {
	Name   string `json:"name"`
	Reader string `json:"reader_symbol"`
	Reason string `json:"reason"`
}

type OperationGuard struct {
	ID        string   `json:"id"`
	Fields    []string `json:"fields"`
	Expected  string   `json:"expected_source"`
	Rejection string   `json:"rejection"`
	Test      string   `json:"test_id"`
}

type OperationWrite struct {
	Stage   string   `json:"stage"`
	Helper  string   `json:"repo_helper"`
	Records []string `json:"affected_records"`
	Test    string   `json:"test_id"`
}

type OperationEffect struct {
	Action   string `json:"action"`
	Recovery string `json:"recovery"`
	Test     string `json:"test_id"`
}

type ContentCoordination struct {
	Required        bool     `json:"required"`
	Reason          string   `json:"reason"`
	DigestSources   []string `json:"digest_sources"`
	Acquire         *string  `json:"acquire_symbol"`
	Release         *string  `json:"release_symbol"`
	ProtectedCommit *string  `json:"protected_commit"`
	Tests           []string `json:"test_ids"`
}

type PolicyRegistry struct {
	SchemaVersion     int            `json:"schemaVersion"`
	InventoryComplete bool           `json:"inventoryComplete"`
	Policies          []PolicyRecord `json:"policies"`
}

type PolicyRecord struct {
	ID          string   `json:"id"`
	Owner       string   `json:"owner"`
	RF          string   `json:"rf_id"`
	Symbol      string   `json:"symbol"`
	Consumers   []string `json:"consumers"`
	Invariant   string   `json:"invariant"`
	Status      string   `json:"status"`
	TargetLayer string   `json:"target_layer"`
	Tests       []string `json:"tests"`
}

type VerificationRegistry struct {
	SchemaVersion int                `json:"schemaVersion"`
	Baseline      string             `json:"baseline"`
	Cases         []VerificationCase `json:"cases"`
}

type VerificationCase struct {
	ID         string          `json:"id"`
	Point      string          `json:"point_id"`
	Tier       string          `json:"tier"`
	File       string          `json:"target_file"`
	Symbol     string          `json:"symbol"`
	Setup      string          `json:"setup_and_action"`
	Assertions string          `json:"required_assertions"`
	Runner     json.RawMessage `json:"runner"`
	Deadline   int             `json:"hard_timeout_seconds"`
	Required   bool            `json:"required"`
}
