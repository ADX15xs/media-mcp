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

// intFromExtra returns v as an int when it is a positive int or float64
// (YAML numbers may arrive as either), otherwise 0.
func intFromExtra(v interface{}) int {
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
	return 0
}

// fracToNumFrames converts a duration in seconds to the nearest Agnes-valid
// frame count: num_frames = 8n + 1 (and <= 441). The result is the closest
// valid frame count to seconds*frameRate (round-half-up on the 4-frame mark).
func fracToNumFrames(seconds, frameRate int) int {
	if seconds <= 0 || frameRate <= 0 {
		return 0
	}
	frames := seconds * frameRate
	// n = round((frames-1)/8); (frames+3)/8 implements round-half-up.
	n := (frames + 3) / 8
	if n < 1 {
		n = 1
	}
	numFrames := 8*n + 1
	if numFrames > 441 {
		numFrames = 441
	}
	return numFrames
}
