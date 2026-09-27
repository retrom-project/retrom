package detector

func stripJSComments(contents []byte) []byte {
	result := make([]byte, 0, len(contents))
	quote := byte(0)
	for position := 0; position < len(contents); {
		current := contents[position]
		if quote != 0 {
			result = append(result, current)
			position++
			if current == '\\' && position < len(contents) {
				result = append(result, contents[position])
				position++
			} else if current == quote {
				quote = 0
			}
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			result = append(result, current)
			position++
			continue
		}
		if current == '/' && position+1 < len(contents) && contents[position+1] == '/' {
			position = skipLineComment(contents, position+2)
			result = append(result, '\n')
			continue
		}
		if current == '/' && position+1 < len(contents) && contents[position+1] == '*' {
			position = skipBlockComment(contents, position+2)
			result = append(result, ' ')
			continue
		}
		result = append(result, current)
		position++
	}
	return result
}

func skipLineComment(contents []byte, position int) int {
	for position < len(contents) && contents[position] != '\n' {
		position++
	}
	return position
}

func skipBlockComment(contents []byte, position int) int {
	for position+1 < len(contents) {
		if contents[position] == '*' && contents[position+1] == '/' {
			return position + 2
		}
		position++
	}
	return len(contents)
}
