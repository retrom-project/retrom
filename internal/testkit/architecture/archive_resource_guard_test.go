package architecture

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type archiveGuardCase struct {
	name  string
	body  string
	extra string
	valid bool
}

func TestArchiveResourceGuardAllowsUnrelatedAssignments(t *testing.T) {
	cases := []archiveGuardCase{
		{name: "switch-case", body: `switch{case ctx.Err()!=nil:err:=ctx.Err();if err!=nil{return nil,err}}`, valid: true},
		{name: "switch-other-case-write", body: `err:=ctx.Err();if err!=nil{switch 1{case 1:return nil,err;case 2:err=nil}}`, valid: true},

		{name: "later-assignment", body: `err:=ctx.Err();if err!=nil{return nil,err};err=ctx.Err();_ = err`, valid: true},
		{name: "sequential-guards", body: `err:=ctx.Err();if err!=nil{return nil,err};_,err=guardResult(ctx);if err!=nil{return nil,err}`, extra: `func guardResult(ctx context.Context)(int,error){return 0,ctx.Err()}`, valid: true},
		{name: "earlier-assignment-rechecked", body: `var err error;err=ctx.Err();if err!=nil{return nil,err}`, valid: true},
		{name: "nested-block", body: `err:=ctx.Err();if err!=nil{{return nil,err}};err=nil`, valid: true},
		{name: "else-guard", body: `err:=ctx.Err();if err==nil{}else{return nil,err};err=nil`, valid: true},
		{name: "if-initializer", body: `if err:=ctx.Err();err!=nil{return nil,err}`, valid: true},
		{name: "distinct-shadow", body: `err:=ctx.Err();if err!=nil{{var err error;_ = err};return nil,err};err=nil`, valid: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { runArchiveGuardCase(t, test) })
	}
}

func TestArchiveResourceGuardRejectsMutableOrDifferentErrors(t *testing.T) {
	cases := []archiveGuardCase{
		{name: "captured-range-callback", body: `err:=ctx.Err();clear:=func(){for _,err=range []error{nil}{}};if err!=nil{clear();return nil,err}`},
		{name: "switch-initializer-write", body: `err:=ctx.Err();if err!=nil{switch err=nil;1{case 1:return nil,err}}`},
		{name: "switch-fallthrough-write", body: `err:=ctx.Err();if err!=nil{switch 1{case 1:err=nil;fallthrough;case 2:return nil,err}}`},

		{name: "direct-nil-write", body: `err:=ctx.Err();if err!=nil{err=nil;return nil,err}`},
		{name: "nested-write", body: `err:=ctx.Err();if err!=nil{if ctx.Err()!=nil{err=nil};return nil,err}`},
		{name: "shadowed-return", body: `err:=ctx.Err();if err!=nil{var err error;return nil,err}`},
		{name: "shadowed-guard", body: `var err error;if err:=ctx.Err();err!=nil{_ = err};if ctx.Err()!=nil{return nil,err}`},
		{name: "different-error", body: `err:=ctx.Err();var other error;if err!=nil{return nil,other}`},
		{name: "address-alias", body: `err:=ctx.Err();if err!=nil{slot:=&err;*slot=nil;return nil,err}`},
		{name: "address-helper", body: `err:=ctx.Err();if err!=nil{clearError(&err);return nil,err}`, extra: `func clearError(value *error){*value=nil}`},
		{name: "captured-callback", body: `err:=ctx.Err();clear:=func(){err=nil};if err!=nil{clear();return nil,err}`},
		{name: "callback-argument", body: `err:=ctx.Err();if err!=nil{invoke(func(){err=nil});return nil,err}`, extra: `func invoke(work func()){work()}`},
		{name: "global-callback", body: `setGlobal(ctx);if globalError!=nil{clearGlobal();return nil,globalError}`, extra: `var globalError error
func setGlobal(ctx context.Context){globalError=ctx.Err()}
func clearGlobal(){globalError=nil}`},
		{name: "goto-reentry", body: `err:=ctx.Err();if err!=nil{again: if err==nil{return nil,err};err=nil;goto again}`},
		{name: "loop-write", body: `err:=ctx.Err();if err!=nil{for{err=nil;return nil,err}}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { runArchiveGuardCase(t, test) })
	}
}

func runArchiveGuardCase(t *testing.T, test archiveGuardCase) {
	t.Helper()
	adapter := strings.ReplaceAll(archiveAdapterFixture, "func openCursor(context.Context)", "func openCursor(ctx context.Context)")
	adapter = strings.ReplaceAll(adapter, "cursor := &cursor{}", test.body+"\n cursor := &cursor{}") + "\n" + test.extra + "\n"
	root, owners := newArchiveFixture(t, archiveFixtureEdit{"internal/adapter/archive/archive.go", "", adapter})
	runtime := runArchiveGuardFixture(t, root, test.valid)
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]PackageOwnership)
	for _, owner := range owners.Packages {
		byPath[owner.Path] = owner
	}
	origin := newArchiveOriginGraph(newArchiveContract(root, graph, byPath))
	guards := []bool{}
	for object, function := range origin.functions {
		if object.Pkg().Path() != "retrom/internal/adapter/archive" || object.Name() != "openCursor" {
			continue
		}
		for _, statement := range function.returns {
			if len(statement.node.Results) == 2 && archiveNil(function.pkg.TypesInfo, statement.node.Results[0]) {
				guards = append(guards, origin.failedReturn(function, statement))
			}
		}
	}
	ports, issues := inspectPortGraph(root, graph, owners)
	labelArchiveBuild(ports, "default")
	archiveGuardRecord(t, test.name, map[string]any{"adapterSource": adapter, "runtimeLog": runtime, "guardProofs": guards, "ports": ports, "violations": issues})
	if len(guards) == 0 {
		t.Fatal("fixture has no nil resource branch")
	}
	for _, proven := range guards {
		if proven != test.valid {
			t.Errorf("direct error guard proof=%v, want %v", proven, test.valid)
		}
	}
	if test.valid {
		requireArchiveProof(t, ports, issues)
	} else {
		requireArchiveRejected(t, ports, issues, "")
	}
}

func runArchiveGuardFixture(t *testing.T, root string, valid bool) string {
	t.Helper()
	expected := `if reader!=nil||err!=nil{t.Fatalf("expected actual nil resource without failure; got %T / %v",reader,err)}`
	if valid {
		expected = `if reader!=nil||!errors.Is(err,context.Canceled){t.Fatalf("expected actual canceled failure; got %T / %v",reader,err)}`
	}
	source := `package archive
import("context";"errors";"testing")
func TestExecutedGuard(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 reader,err:=New().Open(ctx)
 ` + expected + `
 reader,err=New().Open(context.Background())
 if err!=nil||reader==nil{t.Fatalf("success branch=%T / %v",reader,err)}
 if err:=reader.Close();err!=nil{t.Fatal(err)}
 _ = errors.Is
}
`
	writeInventoryFile(t, root, "internal/adapter/archive/guard_runtime_test.go", source)
	command := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-v", "./...")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute actual guard fixture: %v\n%s", err, output)
	}
	return string(output)
}

func archiveGuardRecord(t *testing.T, name string, value any) {
	t.Helper()
	directory := os.Getenv("RETROM_ARCHIVE_GUARD_EVIDENCE")
	if directory == "" {
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
