package serversource

// Root identifies the filesystem used for directory selection and import.
type Root struct {
	ID    string
	Label string
	Path  string
}

// FilesystemRoots exposes the server filesystem to authenticated administrators.
// Operating-system permissions determine which directories can be read.
func FilesystemRoots() []Root {
	return []Root{{ID: "filesystem", Label: "服务器文件系统", Path: "/"}}
}
