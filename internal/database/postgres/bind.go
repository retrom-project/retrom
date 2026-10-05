package postgres

import (
	"strconv"
	"strings"
)

// bindQuery numbers the database contract's anonymous parameters after query
// fragments have been composed. All SQL expressions use PostgreSQL syntax.
// Quoted strings, identifiers and comments never contain bind parameters.
func bindQuery(query string) string {
	var out strings.Builder
	out.Grow(len(query))
	parameter := 0
	for index := 0; index < len(query); {
		switch {
		case query[index] == '$' && dollarQuotedEnd(query, index) > index:
			end := dollarQuotedEnd(query, index)
			out.WriteString(query[index:end])
			index = end
		case query[index] == '\'' || query[index] == '"':
			end := quotedEnd(query, index)
			out.WriteString(query[index:end])
			index = end
		case strings.HasPrefix(query[index:], "--"):
			end := strings.IndexByte(query[index:], '\n')
			if end < 0 {
				out.WriteString(query[index:])
				return out.String()
			}
			out.WriteString(query[index : index+end+1])
			index += end + 1
		case strings.HasPrefix(query[index:], "/*"):
			end := commentEnd(query, index)
			out.WriteString(query[index:end])
			index = end
		case query[index] == '?':
			parameter++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(parameter))
			index++
		default:
			out.WriteByte(query[index])
			index++
		}
	}
	return out.String()
}

func quotedEnd(query string, start int) int {
	quote := query[start]
	escaped := quote == '\'' && start > 0 && (query[start-1] == 'E' || query[start-1] == 'e')
	for index := start + 1; index < len(query); index++ {
		if escaped && query[index] == '\\' {
			index++
			continue
		}
		if query[index] != quote {
			continue
		}
		if index+1 < len(query) && query[index+1] == quote {
			index++
			continue
		}
		return index + 1
	}
	return len(query)
}

func dollarQuotedEnd(query string, start int) int {
	end := start + 1
	for end < len(query) && (query[end] == '_' || query[end] >= 'a' && query[end] <= 'z' ||
		query[end] >= 'A' && query[end] <= 'Z' || end > start+1 && query[end] >= '0' && query[end] <= '9') {
		end++
	}
	if end >= len(query) || query[end] != '$' {
		return start
	}
	delimiter := query[start : end+1]
	offset := strings.Index(query[end+1:], delimiter)
	if offset < 0 {
		return len(query)
	}
	return end + 1 + offset + len(delimiter)
}

func commentEnd(query string, start int) int {
	depth := 1
	for index := start + 2; index < len(query)-1; index++ {
		switch query[index : index+2] {
		case "/*":
			depth++
			index++
		case "*/":
			depth--
			if depth == 0 {
				return index + 2
			}
			index++
		}
	}
	return len(query)
}
