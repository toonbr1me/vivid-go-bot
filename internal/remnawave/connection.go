package remnawave

import "time"

// A missing or malformed field means unknown, never "not connected".
func FirstConnection(raw map[string]any) (known bool, first *time.Time) {
	source := raw
	if nested, ok := raw["userTraffic"].(map[string]any); ok {
		source = nested
	}
	value, ok := source["firstConnectedAt"]
	if !ok {
		return false, nil
	}
	if value == nil || value == "" {
		return true, nil
	}
	if value, ok := value.(string); ok {
		if stamp, err := ParseTime(value); err == nil {
			return true, &stamp
		}
	}
	return false, nil
}
