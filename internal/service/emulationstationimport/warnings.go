package emulationstationimport

import "retrom/internal/capability/format/emulationstationmeta"

func BoundedWarnings(values []map[string]any) []map[string]any {
	if len(values) <= emulationstationmeta.MaxWarnings {
		return values
	}
	omitted := 0
	for _, warning := range values[emulationstationmeta.MaxWarnings-1:] {
		if warning["code"] != emulationstationmeta.WarningLimitReached {
			omitted++
			continue
		}
		switch count := warning["omittedCount"].(type) {
		case float64:
			omitted += int(count)
		case int:
			omitted += count
		}
	}
	result := append([]map[string]any(nil), values[:emulationstationmeta.MaxWarnings-1]...)
	return append(result, map[string]any{
		"code": emulationstationmeta.WarningLimitReached, "omittedCount": omitted,
	})
}
