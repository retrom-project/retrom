package storage

import (
	"log/slog"
	"os"
)

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		slog.Error("close source file", "error", err)
	}
}

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Error("close source directory", "error", err)
	}
}
