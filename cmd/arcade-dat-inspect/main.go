// Command arcade-dat-inspect validates a local arcade DAT with the product parser.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"retrom/internal/format/arcadedat"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/arcade-dat-inspect CORE_ID DAT_PATH")
		os.Exit(2)
	}
	file, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	catalog, err := arcadedat.ParseCatalog(context.Background(), file, os.Args[1])
	closeErr := file.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, closeErr)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(catalog.Stats); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
