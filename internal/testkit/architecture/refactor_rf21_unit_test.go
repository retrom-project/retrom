package architecture

import (
	"go/types"
	"strings"
	"testing"
)

func TestRefactorRF21_alias_evasion(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom/fixture\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "contracts.go", `package fixture
import (
	"context"
	"database/sql"
	"io"
)
type Callback = func() error
type Box[T any] struct { Value T }
type Embedded struct { Callback }
type Generic = Box[Callback]
type Nested struct { Payload *[]map[string]Callback }
type SQLAlias = sql.NullString
type HiddenSQL struct { Inner Box[*sql.Tx] }
type Open struct { Payload any }
type Active struct { Payload interface { Apply() error } }
type Returning struct { Factory func() interface { Apply() error } }
type Streaming struct { Stream io.Reader }
type Context struct { Context context.Context }
type Channel struct { Replies chan int }
type Pure struct { ID string; Flags []bool; Fields map[string]*int64; Next *Pure }
`)
	graph, err := loadInventoryGraph(t.Context(), root, []string{"."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	scope := graph[0].Types.Scope()
	cases := map[string]string{
		"Callback": "executable callback", "Embedded": "executable callback",
		"Generic": "executable callback", "Nested": "executable callback",
		"SQLAlias": "SQL capability", "HiddenSQL": "SQL capability",
		"Open": "open interface", "Active": "capability interface",
		"Returning": "executable callback", "Streaming": "capability interface",
		"Context": "context or implicit clock", "Channel": "channel",
	}
	for name, expected := range cases {
		t.Run(name, func(t *testing.T) {
			issues := InspectValueType(scope.Lookup(name).Type())
			if len(issues) != 1 || !strings.Contains(issues[0].Kind, expected) || len(issues[0].Path) == 0 {
				t.Fatalf("%s evaded recursive contract inspection: %+v", name, issues)
			}
		})
	}
	if issues := InspectValueType(scope.Lookup("Pure").Type()); len(issues) != 0 {
		t.Fatalf("recursive pure values rejected: %+v", issues)
	}
}

func TestValueGraphRejectsUninstantiatedGenerics(t *testing.T) {
	t.Parallel()
	parameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), types.NewInterfaceType(nil, nil).Complete())
	if issues := InspectValueType(parameter); len(issues) != 1 {
		t.Fatalf("unresolved generic accepted: %+v", issues)
	}
}

func TestRefactorRF21_purity_evasion(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/model/value/policy.go", `package value
import (
	"crypto/sha256"
	"time"
	"retrom/internal/capability/helper"
)
type Clock interface { NowMS() int64 }
func Pure(input []byte) [32]byte { return sha256.Sum256(input) }
func HiddenFile() ([]byte, error) { return helper.Read() }
func HiddenClock() int64 { return helper.Now() }
func Background() { go func() {}() }
func init() { _ = time.Now() }
var Prepared = helper.Now()
`)
	writeInventoryFile(t, root, "internal/capability/helper/helper.go", `package helper
import ("os"; "time")
func Read() ([]byte, error) { return os.ReadFile("input") }
func Now() int64 { return time.Now().UnixMilli() }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value"},
		{Path: "internal/capability/helper", Layer: "capability", Module: "helper"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	functions := inspectFunctionGraph(root, graph, owners)
	violations := inspectExecutionRules(functions)
	required := map[string]bool{"HiddenFile": false, "HiddenClock": false, "Background": false, "init@": false, "<init>@": false}
	for _, violation := range violations {
		if strings.HasSuffix(violation.Symbol, ".Pure") {
			t.Fatalf("pure hashing or a port declaration was classified as execution: %+v", violation)
		}
		for key := range required {
			if strings.Contains(violation.Symbol, key) {
				required[key] = true
			}
		}
		if strings.HasSuffix(violation.Symbol, ".HiddenFile") && len(violation.DependencyChain) < 3 {
			t.Fatalf("helper chain missing: %+v", violation)
		}
	}
	for key, found := range required {
		if !found {
			t.Errorf("missed purity violation in %s: %+v", key, violations)
		}
	}
}
