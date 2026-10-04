package specapi

import (
	"encoding/json"
	"reflect"
)

func StatusMatches(existing any, desired map[string]any) bool {
	encoded, err := json.Marshal(existing)
	if err != nil {
		return false
	}
	current := map[string]any{}
	if err := json.Unmarshal(encoded, &current); err != nil {
		return false
	}
	normalized, ok := normalize(desired).(map[string]any)
	if !ok {
		return false
	}
	for key, value := range normalized {
		if !reflect.DeepEqual(current[key], value) {
			return false
		}
	}
	return true
}

func normalize(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return value
	}
	return out
}
