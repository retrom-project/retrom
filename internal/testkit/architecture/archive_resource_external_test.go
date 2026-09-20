package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveResourceExternalGlobalFixedCodec(t *testing.T) {
	root, owners := archiveExternalRealCodec(t)
	archiveExternalRuntime(t, root, `service:=composition.Build();if err:=service.Run(context.Background());err!=nil{t.Fatal(err)}`)
	writeInventoryFile(t, root, "internal/adapter/archive/decoded_test.go", `package archive
import("context";"testing")
func TestRealDecodedName(t *testing.T){reader,err:=New().Open(context.Background());if err!=nil{t.Fatal(err)};defer reader.Close();header,err:=reader.Next();if err!=nil||header.Entry.NormalizedPath!="中"{t.Fatalf("header=%+v err=%v",header,err)}}
`)
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "fixed-real-codec", ports, issues)
	proof := requireArchiveProof(t, ports, issues)
	requireArchiveExternalEvidence(t, proof)
}

func TestArchiveResourceExternalGlobalClosedStringHelper(t *testing.T) {
	root, owners := archiveExternalRealCodec(t, archiveFixtureEdit{
		"internal/capability/format/zipentry/name.go", "", `package zipentry
import "strings"
func DecodeName(value string,nonUTF8 bool)(string,error){return strings.ToUpper(value),nil}
`,
	})
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, issues)
}

func TestArchiveResourceExternalGlobalRealBusinessControls(t *testing.T) {
	for _, mode := range []string{"direct-rewrite", "address-alias-rewrite", "address-escape", "pure-result-business-call"} {
		t.Run(mode, func(t *testing.T) {
			edits := archiveExternalBusinessEdits(mode)
			root, owners := archiveExternalRealCodec(t, edits...)
			assertion := `if err:=composition.Build().Run(context.Background());err==nil{t.Fatal("expected name validation failure")};if composition.BusinessCalls()!=1{t.Fatal("business decoder was not executed")}`
			if mode == "pure-result-business-call" {
				assertion = `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=1{t.Fatal("business helper was not executed")}`
			}
			archiveExternalRuntime(t, root, assertion)
			ports, issues := inspectArchiveFixture(t, root, owners, "default")
			archiveExternalRecord(t, mode, ports, issues)
			requireArchiveRejected(t, ports, issues, "")
		})
	}
}

func archiveExternalBusinessEdits(mode string) []archiveFixtureEdit {
	business := `package libraryimport
import "golang.org/x/text/encoding"
var BusinessCalls int
type BusinessEncoding struct{}
func(BusinessEncoding)NewDecoder()*encoding.Decoder{BusinessCalls++;return encoding.Nop.NewDecoder()}
func(BusinessEncoding)NewEncoder()*encoding.Encoder{return encoding.Nop.NewEncoder()}
func BusinessSideEffect(){BusinessCalls++}
`
	creation := `package composition
import("retrom/internal/adapter/archive";service "retrom/internal/service/libraryimport";"golang.org/x/text/encoding/simplifiedchinese")
func Build()*service.Service{simplifiedchinese.GB18030=service.BusinessEncoding{};return service.New(archive.New())}
func BusinessCalls()int{return service.BusinessCalls}
`
	switch mode {
	case "address-alias-rewrite":
		creation = strings.ReplaceAll(creation, "simplifiedchinese.GB18030=service.BusinessEncoding{}", "alias:=&simplifiedchinese.GB18030;*alias=service.BusinessEncoding{}")
	case "address-escape":
		creation = strings.ReplaceAll(creation, "import(", "import(\"golang.org/x/text/encoding\";")
		creation = strings.ReplaceAll(creation, "simplifiedchinese.GB18030=service.BusinessEncoding{}", "replace(&simplifiedchinese.GB18030)")
		creation += "\nfunc replace(slot *encoding.Encoding){*slot=service.BusinessEncoding{}}\n"
	case "pure-result-business-call":
		creation = archiveBootstrapFixture + "\nfunc BusinessCalls()int{return service.BusinessCalls}\n"
	}
	edits := []archiveFixtureEdit{
		{"internal/service/libraryimport/business.go", "", business},
		{"internal/bootstrap/composition/creation.go", "", creation},
	}
	if mode == "pure-result-business-call" {
		edits = append(edits, archiveFixtureEdit{
			"internal/capability/format/zipentry/name.go", "", `package zipentry
import service "retrom/internal/service/libraryimport"
func DecodeName(value string,nonUTF8 bool)(string,error){service.BusinessSideEffect();return value,nil}
`,
		})
	}
	return edits
}

func TestArchiveResourceExternalGlobalFixedModule(t *testing.T) {
	root, owners := archiveExternalFixedFixture(t, archiveExternalModuleFixture)
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "fixed-module", ports, issues)
	proof := requireArchiveProof(t, ports, issues)
	requireArchiveExternalEvidence(t, proof)
}

func TestArchiveResourceExternalGlobalRejectsUnknownSources(t *testing.T) {
	cases := []struct{ name, source, call string }{
		{"variadic-implementation", strings.ReplaceAll(archiveExternalModuleFixture, "NewDecoder()", "NewDecoder(unused ...string)"), archiveExternalCallFixture},
		{"generic-implementation", strings.NewReplacer("type Config struct", "type Config[T any] struct", "config *Config)", "config *Config[T])", "configuration = Config{", "configuration = Config[int]{").Replace(archiveExternalModuleFixture), archiveExternalCallFixture},
		{"external-global-reader-in-method", strings.ReplaceAll(archiveExternalModuleFixture, "return decoder.Transformer.Transform(value)", "_,_=Input.Read(nil);return decoder.Transformer.Transform(value)") + `type reader struct{}
func(reader)Read([]byte)(int,error){return 0,nil}
var Input=reader{}
`, archiveExternalCallFixture},
		{"external-executable-field-write", strings.ReplaceAll(archiveExternalModuleFixture, "return decoder.Transformer.Transform(value)", "touch(decoder);return decoder.Transformer.Transform(value)") + `func touch(decoder *Decoder){decoder.Transformer=identity{}}
`, archiveExternalCallFixture},
		{"external-executable-field-address", strings.ReplaceAll(archiveExternalModuleFixture, "return decoder.Transformer.Transform(value)", "touch(decoder);return decoder.Transformer.Transform(value)") + `func touch(decoder *Decoder){slot:=&decoder.Transformer;*slot=identity{}}
`, archiveExternalCallFixture},
		{"external-executable-parameter-write", strings.ReplaceAll(archiveExternalModuleFixture, "return decoder.Transformer.Transform(value)", "return echo(decoder.Transformer,value)") + `func echo(transformer Transformer,value string)(string,error){transformer=identity{};return transformer.Transform(value)}
`, archiveExternalCallFixture},
		{"dynamic-initializer", strings.ReplaceAll(archiveExternalModuleFixture, "var Legacy Codec = &configuration", "var Legacy Codec = create()\nfunc create()Codec{return &configuration}"), archiveExternalCallFixture},
		{"callback-in-external-helper", strings.ReplaceAll(archiveExternalModuleFixture, "return value,nil}", "return dynamic(value)}\nvar dynamic = func(value string)(string,error){return value,nil}"), archiveExternalCallFixture},
		{"local-interface-alias", archiveExternalModuleFixture, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder()", "alias:=codec.Legacy;return alias.NewDecoder()")},
		{"intermediate-executable-escape", archiveExternalModuleFixture, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder().String(value)", "decoder:=codec.Legacy.NewDecoder();return decoder.String(value)")},
		{"global-reader", archiveExternalModuleFixture + `type Reader interface{Read([]byte)(int,error)}
type reader struct{}
func(reader)Read([]byte)(int,error){return 0,nil}
var Input Reader=reader{}
`, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder().String(value)", "_,err:=codec.Input.Read(nil);return value,err")},
		{"concrete-global-reader", archiveExternalModuleFixture + `type reader struct{}
func(reader)Read([]byte)(int,error){return 0,nil}
var Input=reader{}
`, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder().String(value)", "_,err:=codec.Input.Read(nil);return value,err")},
		{"concrete-global-resource-factory", archiveExternalModuleFixture + `type Reader interface{Read([]byte)(int,error)}
type reader struct{}
func(reader)Read([]byte)(int,error){return 0,nil}
type factory struct{}
func(factory)Open()Reader{return reader{}}
var Source=factory{}
`, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder().String(value)", "_,err:=codec.Source.Open().Read(nil);return value,err")},
		{"global-resource-factory", archiveExternalModuleFixture + `type Reader interface{Read([]byte)(int,error)}
type reader struct{}
func(reader)Read([]byte)(int,error){return 0,nil}
type Factory interface{Open()Reader}
type factory struct{}
func(factory)Open()Reader{return reader{}}
var Source Factory=factory{}
`, strings.ReplaceAll(archiveExternalCallFixture, "return codec.Legacy.NewDecoder().String(value)", "_,err:=codec.Source.Open().Read(nil);return value,err")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, owners := archiveExternalFixedFixture(t, test.source, archiveFixtureEdit{"internal/capability/format/zipentry/name.go", "", test.call})
			archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
			ports, issues := inspectArchiveFixture(t, root, owners, "default")
			archiveExternalRecord(t, test.name, ports, issues)
			requireArchiveRejected(t, ports, issues, "")
		})
	}
}

func TestArchiveResourceExternalGlobalTaggedWrite(t *testing.T) {
	source := archiveExternalModuleFixture
	root, owners := archiveExternalFixedFixture(t, source)
	writeInventoryFile(t, root, "internal/bootstrap/composition/bad.go", `//go:build integration

package composition
import codec "retrom.test/archivecodec"
func init(){alias:=&codec.Legacy;*alias=nil}
`)
	normal, normalIssues := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, normal, normalIssues)
	tagged, taggedIssues := inspectArchiveFixture(t, root, owners, "integration")
	archiveExternalRecord(t, "integration-write", tagged, taggedIssues)
	requireArchiveRejected(t, tagged, taggedIssues, "")
}

func TestArchiveResourceExternalGlobalExecutableArgument(t *testing.T) {
	source := strings.ReplaceAll(archiveExternalModuleFixture, "NewDecoder()", "NewDecoder(transformer Transformer)")
	source = strings.ReplaceAll(source, "return &Decoder{Transformer:config.Transformer}", "config.Transformer=transformer;return &Decoder{Transformer:config.Transformer}")
	call := `package zipentry
import(codec "retrom.test/archivecodec";service "retrom/internal/service/libraryimport")
func DecodeName(value string,nonUTF8 bool)(string,error){return codec.Legacy.NewDecoder(service.BusinessTransformer{}).String(value)}
`
	root, owners := archiveExternalFixedFixture(t, source,
		archiveFixtureEdit{"internal/capability/format/zipentry/name.go", "", call},
		archiveFixtureEdit{"internal/service/libraryimport/business.go", "", `package libraryimport
var BusinessCalls int
type BusinessTransformer struct{}
func(BusinessTransformer)Transform(value string)(string,error){BusinessCalls++;return value,nil}
`},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", archiveBootstrapFixture + "\nfunc BusinessCalls()int{return service.BusinessCalls}\n"},
	)
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=1{t.Fatal("injected transformer was not executed")}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "executable-argument", ports, issues)
	requireArchiveRejected(t, ports, issues, "")
}

func requireArchiveExternalEvidence(t *testing.T, proof ArchiveResourceProof) {
	t.Helper()
	if len(proof.ExternalSources) == 0 {
		t.Fatal("missing fixed external source evidence")
	}
	for _, source := range proof.ExternalSources {
		if source.Module == "" || source.Version == "" || !safeRepositoryPath(source.Path) || len(source.SHA256) != 64 {
			t.Fatalf("incomplete external evidence: %+v", source)
		}
	}
}

func TestArchiveResourceExternalGlobalRejectsCacheTampering(t *testing.T) {
	root, owners := archiveExternalFixedFixture(t, archiveExternalModuleFixture)
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("GOMODCACHE"), archiveExternalModulePath+"@v1.0.0", "codec.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(source, []byte("\n// locally changed after compiling the fixed module\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	ports, issues := inspectPortGraph(root, graph, owners)
	archiveExternalRecord(t, "cache-tampering", ports, issues)
	requireArchiveRejected(t, ports, issues, "differs from its fixed checksum")
}

func TestArchiveResourceExternalGlobalAggregateAliasMutation(t *testing.T) {
	root, owners := archiveExternalFixedFixture(t, archiveExternalModuleFixture,
		archiveFixtureEdit{"internal/service/libraryimport/business.go", "", `package libraryimport
var BusinessCalls int
type BusinessTransformer struct{}
func(BusinessTransformer)Transform(value string)(string,error){BusinessCalls++;return value,nil}
`},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import("retrom/internal/adapter/archive";service "retrom/internal/service/libraryimport";codec "retrom.test/archivecodec")
func Build()*service.Service{alias:=codec.All[0].(*codec.Config);alias.Transformer=service.BusinessTransformer{};return service.New(archive.New())}
func BusinessCalls()int{return service.BusinessCalls}
`},
	)
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=1{t.Fatal("aggregate alias mutation did not reach the real decoder")}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "aggregate-alias-mutation", ports, issues)
	requireArchiveRejected(t, ports, issues, "")
}

func TestArchiveResourceExternalGlobalModuleInitialization(t *testing.T) {
	changes := map[string]string{
		"init-direct-write":    `func init(){configuration.Transformer=effectful{}}`,
		"init-alias-write":     `func init(){alias:=&configuration;alias.Transformer=effectful{}}`,
		"init-aggregate-alias": `func init(){alias:=All[0].(*Config);alias.Transformer=effectful{}}`,
		"initializer-helper-write": `var initialized=initialize()
func initialize()bool{configuration.Transformer=effectful{};return true}`,
		"initializer-helper-alias": `var initialized=initialize()
func initialize()bool{replace(&configuration);return true}
func replace(config *Config){config.Transformer=effectful{}}`,
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			source := archiveExternalModuleFixture + `
var EffectCalls int
type effectful struct{}
func(effectful)Transform(value string)(string,error){EffectCalls++;return value,nil}
` + change
			creation := strings.ReplaceAll(archiveBootstrapFixture, `import (`, `import (codec "retrom.test/archivecodec";`) + `func EffectCalls()int{return codec.EffectCalls}`
			root, owners := archiveExternalFixedFixture(t, source, archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", creation})
			archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.EffectCalls()!=1{t.Fatal("module initialization did not reach the actual decoder")}`)
			ports, issues := inspectArchiveFixture(t, root, owners, "default")
			archiveExternalRecord(t, name, ports, issues)
			requireArchiveRejected(t, ports, issues, "")
		})
	}
}

func TestArchiveResourceExternalGlobalConcreteDecoder(t *testing.T) {
	source := archiveExternalModuleFixture + `
type concreteDecoder struct{}
func(concreteDecoder)String(value string)(string,error){return Callback(value)}
var Standalone=concreteDecoder{}
var Callback=func(value string)(string,error){return value,nil}
`
	call := `package zipentry
import codec "retrom.test/archivecodec"
func DecodeName(value string,nonUTF8 bool)(string,error){return codec.Standalone.String(value)}
`
	root, owners := archiveExternalFixedFixture(t, source,
		archiveFixtureEdit{"internal/capability/format/zipentry/name.go", "", call},
		archiveFixtureEdit{"internal/service/libraryimport/business.go", "", `package libraryimport
var BusinessCalls int
func BusinessTransform(value string)(string,error){BusinessCalls++;return value,nil}
`},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import("retrom/internal/adapter/archive";service "retrom/internal/service/libraryimport";codec "retrom.test/archivecodec")
func Build()*service.Service{codec.Callback=service.BusinessTransform;return service.New(archive.New())}
func BusinessCalls()int{return service.BusinessCalls}
`})
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)};if composition.BusinessCalls()!=1{t.Fatal("concrete decoder did not execute the business callback")}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "concrete-global-decoder", ports, issues)
	requireArchiveRejected(t, ports, issues, "")
}

func TestArchiveResourceExternalGlobalConcreteClosedValue(t *testing.T) {
	source := archiveExternalModuleFixture + `
type concreteDecoder struct{}
func(concreteDecoder)String(value string)(string,error){return value,nil}
var Standalone=concreteDecoder{}
`
	call := `package zipentry
import codec "retrom.test/archivecodec"
func DecodeName(value string,nonUTF8 bool)(string,error){return codec.Standalone.String(value)}
`
	root, owners := archiveExternalFixedFixture(t, source, archiveFixtureEdit{"internal/capability/format/zipentry/name.go", "", call})
	archiveExternalRuntime(t, root, `if err:=composition.Build().Run(context.Background());err!=nil{t.Fatal(err)}`)
	ports, issues := inspectArchiveFixture(t, root, owners, "default")
	archiveExternalRecord(t, "concrete-closed-value", ports, issues)
	requireArchiveExternalEvidence(t, requireArchiveProof(t, ports, issues))
}
