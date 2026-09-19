package architecture

import (
	"go/types"
	"slices"
	"strings"
)

func callEffect(function *types.Func) string {
	if function.Pkg() == nil {
		return ""
	}
	pkg, name := function.Pkg().Path(), function.Name()
	if sqlExecutionMethod(function) {
		return "SQL execution capability"
	}
	for prefix, reason := range map[string]string{
		"crypto/rand": "random source", "math/rand": "random source",
		"os/exec": "process execution", "log": "process output",
		"unsafe": "unsafe operation", "reflect": "dynamic construction",
	} {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return reason
		}
	}
	if effect := standardCallEffect(pkg, name); effect != "" {
		return effect
	}
	signature, ok := function.Type().(*types.Signature)
	if ok && signature.Recv() != nil {
		if _, capability := signature.Recv().Type().Underlying().(*types.Interface); capability {
			if !streamPort(signature.Recv().Type()) && !slices.Contains([]string{"error", "hash", "hash/crc32"}, pkg) {
				return "interface capability execution"
			}
		}
	}
	return ""
}

type standardEffectGroup struct {
	Package string
	Names   []string
	Effect  string
}

var standardEffects = []standardEffectGroup{
	{
		"time",
		[]string{"Now", "Since", "Until", "After", "AfterFunc", "Tick", "NewTicker", "NewTimer", "Sleep"},
		"global clock or timer",
	},
	{
		"path/filepath",
		[]string{"Abs", "Glob", "Walk", "WalkDir", "EvalSymlinks"},
		"filesystem or working directory access",
	},
	{
		"context",
		[]string{"WithDeadline", "WithDeadlineCause", "WithTimeout", "WithTimeoutCause", "AfterFunc"},
		"global clock or timer",
	},
	{
		"fmt",
		[]string{"Print", "Printf", "Println", "Scan", "Scanf", "Scanln", "Fprint", "Fprintf", "Fprintln"},
		"process or writer I/O",
	},
	{"io", []string{"Pipe"}, "concurrent pipe"},
	{"archive/zip", []string{"OpenReader"}, "filesystem access"},
}

func standardCallEffect(pkg, name string) string {
	for _, group := range standardEffects {
		if pkg == group.Package && slices.Contains(group.Names, name) {
			return group.Effect
		}
	}
	osPure := []string{"IsNotExist", "IsExist", "IsPermission", "IsTimeout", "NewSyscallError"}
	if pkg == "os" && !slices.Contains(osPure, name) {
		return "filesystem or environment access"
	}
	if slices.Contains([]string{"net/http", "net/rpc", "net/smtp"}, pkg) {
		return "network or protocol operation"
	}
	if pkg == "net" || pkg == "net/url" {
		if strings.HasPrefix(name, "Dial") || strings.HasPrefix(name, "Listen") || strings.HasPrefix(name, "Lookup") {
			return "network operation"
		}
	}
	return ""
}

func sqlExecutionMethod(function *types.Func) bool {
	if function.Pkg() == nil {
		return false
	}
	names := []string{
		"Exec", "ExecContext", "Query", "QueryContext", "QueryRow", "QueryRowContext",
		"Prepare", "PrepareContext", "Begin", "BeginTx", "Commit", "Rollback", "Raw",
	}
	if !slices.Contains(names, function.Name()) {
		return false
	}
	pkg := function.Pkg().Path()
	return pkg == "database/sql" || pkg == "database/sql/driver" || strings.HasSuffix(pkg, "/internal/repo/dbexec")
}
