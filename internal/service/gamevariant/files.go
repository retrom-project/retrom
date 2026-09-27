package gamevariant

func productFile(files []File, role, name string) (File, bool) {
	for _, file := range files {
		if file.Role == role && (name == "" || file.LogicalName == name) {
			return file, true
		}
	}
	return File{}, false
}
