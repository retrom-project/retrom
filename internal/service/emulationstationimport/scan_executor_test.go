package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/emulationstationimport"
	"testing"
)

type scanExecutionMemory struct {
	source                                             *scannerMemory
	calls                                              []string
	publishErr, resetErr, rejectErr, errorAfterPublish error
	resets                                             int
}

func (memory *scanExecutionMemory) ForScan(model.Execution) (ScannerSource, error) {
	return memory.source, nil
}

func (memory *scanExecutionMemory) Reset(
	context.Context,
	model.Execution,
) error {
	memory.calls = append(memory.calls, "reset")
	memory.resets++
	if memory.resets > 1 {
		return memory.errorAfterPublish
	}
	return memory.resetErr
}

func (memory *scanExecutionMemory) Rejected(
	context.Context,
	model.Execution,
	model.ScanProjection,
) error {
	memory.calls = append(memory.calls, "rejected")
	return memory.rejectErr
}

func (memory *scanExecutionMemory) Publish(
	context.Context,
	model.Execution,
	model.ScanProjection,
) error {
	memory.calls = append(memory.calls, "publish")
	return memory.publishErr
}

func (memory *scanExecutionMemory) executor() *ScanExecutor { return NewScanExecutor(memory, memory) }

func TestScanExecutorPublishesNormalAndIsolatedInvalidEvidence(t *testing.T) {
	for _, invalid := range []bool{
		false,
		true,
	} {
		t.Run(
			map[bool]string{false: "normal", true: "invalid"}[invalid],
			func(t *testing.T) {
				memory := &scanExecutionMemory{source: newScannerMemory()}
				if invalid {
					memory.source.data["gamelist.xml"] = []byte("<gameList>")
				}
				err := memory.executor().Execute(t.Context(), model.Execution{ReleaseYearMax: 2027})
				expected := []string{"reset", "publish"}
				if invalid {
					expected[1] = "rejected"
					if !errors.Is(err, ErrNoValidGamelist) {
						t.Fatalf("lost parser error=%v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(memory.calls, expected) {
					t.Fatalf("calls=%v", memory.calls)
				}
			},
		)
	}
}

func TestScanExecutorRetainsPublicationAndCleanupCauses(t *testing.T) {
	first, second := errors.New("publication failed"), errors.New("cleanup failed")
	memory := &scanExecutionMemory{source: newScannerMemory(), publishErr: first, errorAfterPublish: second}
	err := memory.executor().Execute(t.Context(), model.Execution{ReleaseYearMax: 2027})
	if !errors.Is(
		err,
		first,
	) || !errors.Is(
		err,
		second,
	) || !reflect.DeepEqual(
		memory.calls,
		[]string{"reset", "publish", "reset"},
	) {
		t.Fatalf(
			"calls=%v error=%v",
			memory.calls,
			err,
		)
	}
}

func TestScanExecutorNeverClearsReplacementPublication(t *testing.T) {
	memory := &scanExecutionMemory{source: newScannerMemory(), publishErr: model.ErrVersionConflict}
	err := memory.executor().Execute(t.Context(), model.Execution{ReleaseYearMax: 2027})
	if !errors.Is(err, model.ErrVersionConflict) || memory.resets != 1 {
		t.Fatalf("resets=%d error=%v", memory.resets, err)
	}
}
