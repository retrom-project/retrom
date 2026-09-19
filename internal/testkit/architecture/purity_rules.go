package architecture

import (
	"slices"
	"strings"
)

func inspectExecutionRules(functions []FunctionInventory) []Violation {
	bySymbol := make(map[string]FunctionInventory)
	for _, function := range functions {
		bySymbol[function.Symbol] = function
	}
	violations := make([]Violation, 0)
	for _, function := range functions {
		if slices.Contains([]string{"model", "capability", "foundation"}, function.Layer) {
			for _, call := range function.Calls {
				chain, effect := firstImpureCall(call, bySymbol, map[string]bool{})
				if effect != "" {
					violations = append(violations, executionViolation("AR05", function, call.Line, chain, effect))
				}
			}
		}
		if !slices.Contains([]string{"repo", "testkit", "tool", "fixture"}, function.Layer) {
			for _, call := range function.Calls {
				if call.Effect == "SQL execution capability" {
					violations = append(violations, executionViolation("AR06", function, call.Line,
						[]string{call.Symbol}, call.Effect))
				}
			}
		}
	}
	return violations
}

func firstImpureCall(
	call CallInventory, functions map[string]FunctionInventory, seen map[string]bool,
) ([]string, string) {
	if call.Effect != "" {
		return []string{call.Symbol}, call.Effect
	}
	if seen[call.Symbol] {
		return nil, ""
	}
	seen[call.Symbol] = true
	function, local := functions[call.Symbol]
	if !local {
		if call.Package != "" && !knownPureStandardPackage(call.Package) {
			return []string{call.Symbol}, "unclassified external call"
		}
		return nil, ""
	}
	for _, child := range function.Calls {
		if chain, effect := firstImpureCall(child, functions, seen); effect != "" {
			return append([]string{call.Symbol}, chain...), effect
		}
	}
	return nil, ""
}

func knownPureStandardPackage(pkg string) bool {
	packages := []string{
		"archive/tar", "archive/zip", "bufio", "bytes", "cmp", "context",
		"crypto", "crypto/aes", "crypto/cipher", "crypto/des", "crypto/hmac", "crypto/md5",
		"crypto/sha1", "crypto/sha256", "crypto/sha512", "crypto/subtle",
		"encoding", "errors", "fmt", "hash", "html", "image", "index/suffixarray",
		"io", "io/fs", "iter", "maps", "math", "math/big", "math/bits", "mime",
		"mime/multipart", "net", "net/url", "os", "path", "path/filepath", "regexp",
		"regexp/syntax", "slices", "sort", "strconv", "strings", "time", "unicode",
		"unicode/utf16", "unicode/utf8",
	}
	return slices.Contains(packages, pkg) || strings.HasPrefix(pkg, "encoding/") ||
		strings.HasPrefix(pkg, "hash/") || strings.HasPrefix(pkg, "compress/") || strings.HasPrefix(pkg, "image/")
}

func executionViolation(rule string, function FunctionInventory, line int, chain []string, effect string) Violation {
	return Violation{
		Rule: rule, File: function.File, Line: line, Symbol: function.Symbol,
		DependencyChain: append([]string{function.Symbol}, chain...), Message: effect + " is forbidden in " + function.Layer,
	}
}
