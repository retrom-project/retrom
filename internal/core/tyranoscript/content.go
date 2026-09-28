package tyranoscript

import (
	"path"
	"strings"
)

func ProjectMediaType(logicalName string) (string, bool) {
	mediaTypes := map[string]string{
		".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
		".js": "application/javascript; charset=utf-8", ".mjs": "application/javascript; charset=utf-8",
		".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8",
		".ks": "text/plain; charset=utf-8", ".tjs": "text/plain; charset=utf-8",
		".txt": "text/plain; charset=utf-8", ".csv": "text/csv; charset=utf-8",
		".xml": "application/xml; charset=utf-8", ".svg": "image/svg+xml",
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".webp": "image/webp", ".gif": "image/gif", ".bmp": "image/bmp", ".ico": "image/x-icon",
		".ogg": "audio/ogg", ".opus": "audio/ogg", ".m4a": "audio/mp4",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".mp4": "video/mp4", ".webm": "video/webm",
		".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf",
		".otf": "font/otf", ".eot": "application/vnd.ms-fontobject", ".bin": "application/octet-stream",
	}
	mediaType, exists := mediaTypes[strings.ToLower(path.Ext(logicalName))]
	return mediaType, exists
}
