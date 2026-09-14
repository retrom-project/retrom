package launch

type ProductExternalSnapshot struct {
	DependencySnapshot, ContentName string
	Files                           []ProductExternalFile
}
