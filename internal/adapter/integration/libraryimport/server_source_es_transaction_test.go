//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestOwnedESSourceRollsBackLateBindingFailures(t *testing.T) {
	for _, phase := range []string{"write", "count", "zero", "execution fence"} {
		t.Run(phase, func(t *testing.T) {
			fixture, request := ownedESSourceFixture(t)
			fault := &esSourceBindingFault{phase: phase, cause: errors.New("ES late binding fault")}
			hooks := testsupport.SQLFaultHooks{BeforeExec: fault.before, AfterExec: fault.after}
			fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, hooks)
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			wanted := fault.cause
			if phase == "zero" || phase == "execution fence" {
				wanted = ErrVersionConflict
			}
			if !errors.Is(err, wanted) || result.Created.ImportJobID != "" || result.Items != nil || fault.ordinaryWrites != 1 || !fault.matched {
				t.Fatalf("phase=%s writes=%d matched=%v result=%+v err=%v", phase, fault.ordinaryWrites, fault.matched, result, err)
			}
			assertNoOwnedESImport(t, fixture)
		})
	}
}

func isESSourceBinding(query string) bool {
	return strings.HasPrefix(query, "UPDATE emulationstation_import_items SET") && strings.Contains(query, "library_import_job_id=")
}

type esSourceBindingFault struct {
	phase          string
	cause          error
	ordinaryWrites int
	matched        bool
}

func (fault *esSourceBindingFault) before(_ context.Context, query string, _ []driver.NamedValue) error {
	if fault.phase == "write" && isESSourceBinding(query) {
		fault.matched = true
		return fault.cause
	}
	return nil
}

func (fault *esSourceBindingFault) after(
	_ context.Context, query string, _ []driver.NamedValue, result driver.Result,
) (driver.Result, error) {
	if strings.Contains(query, "INSERT INTO import_items(") {
		fault.ordinaryWrites++
	}
	target := isESSourceBinding(query)
	if fault.phase == "execution fence" {
		target = strings.HasPrefix(query, "UPDATE jobs SET version=version WHERE id=?")
	}
	if !target {
		return result, nil
	}
	fault.matched = true
	switch fault.phase {
	case "count":
		return sourceFaultResult{cause: fault.cause}, nil
	case "zero", "execution fence":
		return driver.RowsAffected(0), nil
	default:
		return result, nil
	}
}
