package runtimeclient

import (
	"log/slog"
	"os"
)

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Error("close Provider root", "error", err)
	}
}

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		slog.Error("close Provider asset", "error", err)
	}
}
