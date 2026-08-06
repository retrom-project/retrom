package migrations

import "embed"

// Files contains the immutable ordered SQL migration sources.
//
//go:embed *.sql
var Files embed.FS
