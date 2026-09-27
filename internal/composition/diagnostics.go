package composition

import (
	dbapi "retrom/internal/database"

	diagnosticspersistence "retrom/internal/persistence/diagnostics"
	diagnosticsservice "retrom/internal/service/diagnostics"
)

// NewDiagnostics wires the diagnostics application service to its persistence adapter.
func NewDiagnostics(database dbapi.DB) *diagnosticsservice.Service {
	return diagnosticsservice.New(diagnosticspersistence.New(database))
}
