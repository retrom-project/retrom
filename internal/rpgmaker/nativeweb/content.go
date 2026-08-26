package nativeweb

import (
	"path"
	"strings"
)

var projectMediaTypes = map[string]string{
	".js": "application/javascript; charset=utf-8", ".mjs": "application/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8",
	".wasm": "application/wasm", ".txt": "text/plain; charset=utf-8", ".csv": "text/csv; charset=utf-8",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif",
	".ogg": "audio/ogg", ".opus": "audio/ogg", ".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".wav": "audio/wav",
	".mp4": "video/mp4", ".webm": "video/webm",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".eot":    "application/vnd.ms-fontobject",
	".rpgmvp": "application/octet-stream", ".rpgmvo": "application/octet-stream", ".rpgmvm": "application/octet-stream",
	".png_": "application/octet-stream", ".ogg_": "application/octet-stream", ".m4a_": "application/octet-stream",
}

var nativeExecutableExtensions = map[string]struct{}{
	".bat": {}, ".cmd": {}, ".com": {}, ".dll": {}, ".dylib": {},
	".exe": {}, ".node": {}, ".ps1": {}, ".so": {},
}

func ProjectMediaType(logicalName string) (string, bool) {
	mediaType, exists := projectMediaTypes[strings.ToLower(path.Ext(logicalName))]
	return mediaType, exists
}

func RuntimeFile(logicalName string) bool {
	if logicalName == "index.html" {
		return true
	}
	if strings.EqualFold(logicalName, "package.json") {
		return false
	}
	_, allowed := ProjectMediaType(logicalName)
	return allowed
}

func NativeExecutable(logicalName string) bool {
	_, executable := nativeExecutableExtensions[strings.ToLower(path.Ext(logicalName))]
	return executable
}
