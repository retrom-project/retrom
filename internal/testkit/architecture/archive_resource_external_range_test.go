package architecture

import (
	"strings"
	"testing"
)

func TestArchiveResourceExternalGlobalRangeRejectsUnknownWrites(t *testing.T) {
	cases := []struct {
		name, body string
		business   bool
	}{
		{
			name: "field-assignment",
			body: `for _, decoder.Transformer = range []Transformer{effectful{}} {}
return decoder.Transformer.Transform(value)`,
			business: true,
		},
		{
			name: "local-assignment",
			body: `picked := decoder.Transformer
for _, picked = range []Transformer{effectful{}} {}
return picked.Transform(value)`,
			business: true,
		},
		{
			name: "executable-definition",
			body: `for _, picked := range []Transformer{effectful{}} {
return picked.Transform(value)
}
return value,nil`,
			business: true,
		},
		{
			name: "index-target",
			body: `var positions [1]int
for _, positions[0] = range []int{1} {}
return decoder.Transformer.Transform(value)`,
		},
		{
			name: "dereference-target",
			body: `position := 0
for _, *(&position) = range []int{1} {}
return decoder.Transformer.Transform(value)`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, owners := archiveExternalRangeFixture(t, test.body)
			assertion := `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=0{t.Fatal("unexpected callback")}`
			if test.business {
				assertion = `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=1{t.Fatal("range write did not execute the business callback")}`
			}
			archiveExternalRuntime(t, root, assertion)
			ports, issues := inspectArchiveFixture(t, root, owners, "default")
			archiveExternalRecord(t, "range-reject-"+test.name, ports, issues)
			requireArchiveRejected(t, ports, issues, "")
		})
	}
}

func TestArchiveResourceExternalGlobalRangePureValues(t *testing.T) {
	bodies := map[string]string{
		"definition": `for _, item := range []string{value} { value=item }
return decoder.Transformer.Transform(value)`,
		"assignment": `for _, value = range []string{value} {}
return decoder.Transformer.Transform(value)`,
		"shadow-executable-name": `picked := decoder.Transformer
for picked := range []int{0} { _=picked }
return picked.Transform(value)`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			root, owners := archiveExternalRangeFixture(t, body)
			archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=0{t.Fatal("pure range invoked a callback")}`)
			ports, issues := inspectArchiveFixture(t, root, owners, "default")
			archiveExternalRecord(t, "range-pure-"+name, ports, issues)
			requireArchiveExternalEvidence(t, requireArchiveProof(t, ports, issues))
		})
	}
}

func archiveExternalRangeFixture(t *testing.T, body string) (string, OwnershipRegistry) {
	t.Helper()
	source := strings.ReplaceAll(archiveExternalModuleFixture, "return decoder.Transformer.Transform(value)", body)
	source += `
type effectful struct{}
func(effectful)Transform(value string)(string,error){return Callback(value)}
var Callback=func(value string)(string,error){return value,nil}
`
	return archiveExternalFixedFixture(t, source,
		archiveFixtureEdit{"internal/service/libraryimport/business.go", "", `package libraryimport
var BusinessCalls int
func BusinessTransform(value string)(string,error){BusinessCalls++;return value,nil}
`},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import("retrom/internal/adapter/archive";service "retrom/internal/service/libraryimport";codec "retrom.test/archivecodec")
func Build()*service.Service{codec.Callback=service.BusinessTransform;return service.New(archive.New())}
func BusinessCalls()int{return service.BusinessCalls}
`})
}
