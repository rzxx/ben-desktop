package playback

import "time"

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimestamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func normalizeTimestamp(value string) string {
	parsed, ok := parseTimestamp(value)
	if !ok {
		return ""
	}
	return formatTimestamp(parsed)
}
