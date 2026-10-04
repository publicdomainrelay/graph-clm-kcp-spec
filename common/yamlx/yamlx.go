package yamlx

import (
	"bytes"
	"encoding/json"
	"fmt"

	yaml "go.yaml.in/yaml/v2"
)

// Marshal renders the same bytes sigs.k8s.io/yaml does, except that object keys
// come out in the order json.Marshal wrote them. sigs.k8s.io/yaml hands a Go
// map to yaml.v2, whose key sort is not a strict weak ordering for keys holding
// a colon and a hex digest, so the same state rendered twice can differ; the
// branch decisions are blob equality, so that is not cosmetic.
func Marshal(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("yamlx: marshal to JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	ordered, err := decode(decoder)
	if err != nil {
		return nil, err
	}
	encoded, err := yaml.Marshal(ordered)
	if err != nil {
		return nil, fmt.Errorf("yamlx: marshal to YAML: %w", err)
	}
	return encoded, nil
}

func decode(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("yamlx: read token: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return scalar(token), nil
	}
	switch delimiter {
	case '{':
		out := yaml.MapSlice{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("yamlx: read key: %w", err)
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("yamlx: object key %v is not a string", key)
			}
			value, err := decode(decoder)
			if err != nil {
				return nil, err
			}
			out = append(out, yaml.MapItem{Key: name, Value: value})
		}
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("yamlx: close object: %w", err)
		}
		return out, nil
	case '[':
		out := []any{}
		for decoder.More() {
			value, err := decode(decoder)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("yamlx: close array: %w", err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("yamlx: unexpected delimiter %v", delimiter)
}

func scalar(token any) any {
	number, ok := token.(json.Number)
	if !ok {
		return token
	}
	if integer, err := number.Int64(); err == nil {
		return integer
	}
	if decimal, err := number.Float64(); err == nil {
		return decimal
	}
	return number.String()
}
