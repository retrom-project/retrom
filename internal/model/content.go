package model

// GameFileMember belongs to one immutable physical package. It has no independent lifecycle.
type GameFileMember struct {
	LogicalPath string
	DataOffset  int64
	SizeBytes   int64
	SHA256      string
	MediaType   string
}

// ContentFile is a logical file, backed by a standalone object or a package interval.
// GameFile's ID/Role describe physical storage only; consumers use logical facts.
type ContentFile struct {
	ID         string    `json:"-"`
	LogicalKey string    `json:"logicalKey"`
	Role       string    `json:"-"`
	StorageKey string    `json:"-"`
	SHA256     string    `json:"sha256"`
	SizeBytes  int64     `json:"sizeBytes"`
	Offset     int64     `json:"-"`
	MediaType  string    `json:"mediaType"`
	Package    *GameFile `json:"-"`
}

type ContentPage struct {
	Items      []ContentFile `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}

func StandaloneFiles(files []GameFile) []ContentFile {
	result := make([]ContentFile, 0, len(files))
	for _, file := range files {
		result = append(result, FileContent(file))
	}
	return result
}

func FileContent(file GameFile) ContentFile {
	return ContentFile{
		ID: file.ID, LogicalKey: file.LogicalKey, Role: file.Role,
		StorageKey: file.StorageKey, SHA256: file.SHA256, SizeBytes: file.SizeBytes,
	}
}
