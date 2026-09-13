package composition

import (
	"database/sql"

	diagnosticspersistence "retrom/internal/repo/diagnostics"
	diagnosticsservice "retrom/internal/service/diagnostics"
)

// NewDiagnostics wires the diagnostics application service to its persistence adapter.
func NewDiagnostics(database *sql.DB) *diagnosticsservice.Service {
	return diagnosticsservice.New(diagnosticspersistence.New(database))
}
