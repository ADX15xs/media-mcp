package supplier

// defaultStr returns s if non-empty, otherwise def.
func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// intMax returns v if it's a positive number, otherwise def.
// Accepts float64 (from JSON unmarshaling) or int.
func intMax(v interface{}, def int) int {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return def
}

// truncate shortens a string to max runes, appending an ellipsis if truncated.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...[truncated]"
}

// maxInt calls intMax for backwards compatibility.
func maxInt(v interface{}, def int) int {
	return intMax(v, def)
}
