package model

// Browser upload limits (D1, D8 of the upload design): one file, one session, active sessions.
const (
	UploadMaximumFileBytes  int64 = 4 * 1024 * 1024 * 1024
	UploadMaximumTotalBytes int64 = 20 * 1024 * 1024 * 1024
	UploadMaximumFiles            = 50_000
	UploadMaximumSessions         = 2
	UploadChunkBytes        int64 = 64 * 1024 * 1024
)

type (
	UploadFileInput struct {
		Path      string `json:"path"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	UploadInput struct {
		Format string            `json:"format"`
		Files  []UploadFileInput `json:"files"`
	}
	UploadFile struct {
		Path          string `json:"path"`
		SizeBytes     int64  `json:"sizeBytes"`
		ReceivedBytes int64  `json:"receivedBytes"`
	}
	Upload struct {
		ID             string       `json:"id"`
		Format         string       `json:"format"`
		ChunkSizeBytes int64        `json:"chunkSizeBytes"`
		ExpiresAtMs    int64        `json:"expiresAtMs"`
		Completed      bool         `json:"completed"`
		Files          []UploadFile `json:"files"`
	}
	UploadProgress struct {
		ReceivedBytes int64 `json:"receivedBytes"`
		Completed     bool  `json:"completed"`
	}
)
