package specapi

import (
	"encoding/json"
	"reflect"
)

// StatusMatches reports whether every key of the desired status already has the
// desired value on the object. It is what makes a reconcile a no-op instead of
// a status write, which is what keeps a controller from looping against
// itself, so both ingest and the controller use it.
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
