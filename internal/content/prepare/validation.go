package prepare

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/cleanup"

	"github.com/tetratelabs/wazero"
)

const maxWASM4Bytes = 1 << 16

func (service *Service) validate(ctx context.Context, platformID string, file File) error {
	if file.Size <= 0 {
		return &Invalid{Code: "CONTENT_EMPTY"}
	}
	if platformID != "wasm4" {
		return nil
	}
	if file.Size > maxWASM4Bytes {
		return &Invalid{Code: "WASM4_CART_INVALID"}
	}
	reader, err := service.files.OpenRecord(file.Record)
	if err != nil {
		return fmt.Errorf("open content for validation: %w", err)
	}
	defer func() { cleanup.Error("close content validation reader", reader.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(reader, maxWASM4Bytes+1))
	if err != nil {
		return fmt.Errorf("read cartridge: %w", err)
	}
	if int64(len(contents)) != file.Size {
		return &Invalid{Code: "CONTENT_SIZE_MISMATCH"}
	}
	return validateWASM4(ctx, contents)
}

// CompileModule validates the entire module without instantiating it or running uploaded code.
func validateWASM4(ctx context.Context, contents []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate cartridge: %w", err)
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().
		WithDebugInfoEnabled(false))
	defer func() { cleanup.Error("close cartridge validator", runtime.Close(context.WithoutCancel(ctx))) }()
	module, err := runtime.CompileModule(ctx, contents)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("validate cartridge: %w", ctx.Err())
		}
		return &Invalid{Code: "WASM4_CART_INVALID"}
	}
	if len(module.ImportedMemories()) != 1 {
		return &Invalid{Code: "WASM4_CART_INVALID"}
	}
	for _, memory := range module.ImportedMemories() {
		moduleName, name, _ := memory.Import()
		maximum, bounded := memory.Max()
		if moduleName != "env" || name != "memory" || memory.Min() > 1 || bounded && maximum < 1 {
			return &Invalid{Code: "WASM4_CART_INVALID"}
		}
	}
	for _, function := range module.ImportedFunctions() {
		moduleName, name, _ := function.Import()
		if moduleName != "env" || !wasm4Function(name) {
			return &Invalid{Code: "WASM4_CART_INVALID"}
		}
	}
	return nil
}

func wasm4Function(name string) bool {
	switch name {
	case "rect", "oval", "line", "hline", "vline", "text", "textUtf8", "textUtf16", "blit", "blitSub",
		"tone", "diskr", "diskw", "trace", "traceUtf8", "traceUtf16", "tracef":
		return true
	default:
		return false
	}
}
