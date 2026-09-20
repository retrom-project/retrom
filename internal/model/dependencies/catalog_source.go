package dependencies

import "io"

// DATCatalogSource acquires a built-in DAT stream without parsing or entering a
// write scope. The caller owns Close and supplies the bounded pure parser.
// Open failures retain the established "open built-in DAT" technical context.
type DATCatalogSource interface {
	OpenBuiltIn(root, relativePath string) (io.ReadCloser, error)
}
