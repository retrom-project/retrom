package architecture

// Rule IDs for the layering architecture checker.
const (
	RuleModelRepoNoServiceDep   = "LAYER-001" // model/repo must not depend on service/transport
	RuleServiceNoRepoDep        = "LAYER-002" // service must not depend on concrete repo or SQL
	RulePortNoCallback          = "LAYER-003" // repository port params/returns must not contain func types
	RuleNoHiddenCapability      = "LAYER-004" // commands/plans must not hide capabilities via alias/embed/any
	RuleRepoNoServiceCallback   = "LAYER-005" // repo must not accept/hold model business ports from service
	RuleServiceNoModelReexport  = "LAYER-006" // service must not re-export model types via alias/wrapper
	RuleModelNoPureViolation    = "LAYER-007" // model must not call I/O, clock, random, UUID, env
	RuleNonRepoNoDBCapability   = "LAYER-008" // non-repo code must not obtain dbexec/sql.Tx/Rows/nullable
	RuleRepoCallbackScope       = "LAYER-009" // repo internal callback must not be defined outside repo
	RuleNoEvasion               = "LAYER-010" // must not evade checks via rename/generated/allowlist
)

// Violation represents a single architecture rule violation.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Symbol   string `json:"symbol,omitempty"`
	Message  string `json:"message"`
}

// InventoryItem represents a scanned symbol for the inventory report.
type InventoryItem struct {
	Package  string `json:"package"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // type, func, var, const
	Layer    string `json:"layer"` // model, service, repo, transport, other
	Status   string `json:"status"` // verified, violation, pending
	Rule     string `json:"rule,omitempty"`
}

// ScanResult holds the output of a full architecture scan.
type ScanResult struct {
	Violations []Violation     `json:"violations"`
	Inventory  []InventoryItem `json:"inventory,omitempty"`
	Errors     []string        `json:"errors,omitempty"`
}
