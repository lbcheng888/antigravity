package printmode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// ValidateJSONSchema validates instanceJSON against schemaBytes using pure-Go stdlib JSON decoding.
// It checks type (object, array, string, number, integer, boolean, null), required, properties, items, and enum.
func ValidateJSONSchema(schemaBytes []byte, instanceJSON []byte) error {
	schemaVal, err := decodeJSONWithNumbers(schemaBytes)
	if err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}
	schema, ok := schemaVal.(map[string]any)
	if !ok {
		return fmt.Errorf("schema must be a JSON object")
	}

	instance, err := decodeJSONWithNumbers(instanceJSON)
	if err != nil {
		return fmt.Errorf("invalid instance JSON: %w", err)
	}

	return validateNode("$", schema, instance)
}

func decodeJSONWithNumbers(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return v, nil
}

func validateNode(path string, schema map[string]any, val any) error {
	if rawType, ok := schema["type"]; ok {
		if err := checkType(path, rawType, val); err != nil {
			return err
		}
	}

	if rawEnum, ok := schema["enum"]; ok {
		enumSlice, ok := rawEnum.([]any)
		if !ok {
			return fmt.Errorf("%s: schema enum must be an array", path)
		}
		matched := false
		for _, candidate := range enumSlice {
			if jsonEqual(val, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value %v is not in enum %v", path, val, enumSlice)
		}
	}

	if obj, ok := val.(map[string]any); ok {
		if rawReq, ok := schema["required"]; ok {
			reqSlice, ok := rawReq.([]any)
			if !ok {
				return fmt.Errorf("%s: schema required must be an array", path)
			}
			for _, item := range reqSlice {
				key, ok := item.(string)
				if !ok {
					return fmt.Errorf("%s: schema required entry must be a string", path)
				}
				if _, exists := obj[key]; !exists {
					return fmt.Errorf("%s: missing required property %q", path, key)
				}
			}
		}

		if rawProps, ok := schema["properties"]; ok {
			props, ok := rawProps.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: schema properties must be an object", path)
			}
			for key, subRaw := range props {
				propVal, exists := obj[key]
				if !exists {
					continue
				}
				subSchema, ok := subRaw.(map[string]any)
				if !ok {
					return fmt.Errorf("%s.%s: property schema must be an object", path, key)
				}
				if err := validateNode(path+"."+key, subSchema, propVal); err != nil {
					return err
				}
			}
		}
	}

	if arr, ok := val.([]any); ok {
		if rawItems, ok := schema["items"]; ok {
			itemSchema, ok := rawItems.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: schema items must be an object", path)
			}
			for i, elem := range arr {
				elemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := validateNode(elemPath, itemSchema, elem); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func checkType(path string, rawType any, val any) error {
	var allowed []string
	switch t := rawType.(type) {
	case string:
		allowed = []string{t}
	case []any:
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("%s: schema type array entries must be strings", path)
			}
			allowed = append(allowed, s)
		}
	default:
		return fmt.Errorf("%s: invalid schema type %T", path, rawType)
	}

	for _, expected := range allowed {
		if matchesType(val, expected) {
			return nil
		}
	}
	return fmt.Errorf("%s: expected %s, got %s", path, strings.Join(allowed, "|"), jsonTypeName(val))
}

func matchesType(val any, expected string) bool {
	switch expected {
	case "null":
		return val == nil
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "string":
		_, ok := val.(string)
		return ok
	case "object":
		_, ok := val.(map[string]any)
		return ok
	case "array":
		_, ok := val.([]any)
		return ok
	case "number":
		num, ok := val.(json.Number)
		if !ok {
			return false
		}
		_, err := strconv.ParseFloat(num.String(), 64)
		return err == nil
	case "integer":
		num, ok := val.(json.Number)
		if !ok {
			return false
		}
		if _, err := strconv.ParseInt(num.String(), 10, 64); err == nil {
			return true
		}
		f, err := strconv.ParseFloat(num.String(), 64)
		return err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f
	default:
		return false
	}
}

func jsonTypeName(val any) string {
	switch v := val.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case json.Number:
		if matchesType(v, "integer") {
			return "integer"
		}
		return "number"
	default:
		return fmt.Sprintf("%T", val)
	}
}

func jsonEqual(a, b any) bool {
	na, aNum := a.(json.Number)
	nb, bNum := b.(json.Number)
	if aNum && bNum {
		fa, errA := na.Float64()
		fb, errB := nb.Float64()
		if errA == nil && errB == nil {
			return fa == fb
		}
		return na.String() == nb.String()
	}
	if aNum != bNum {
		return false
	}

	switch va := a.(type) {
	case []any:
		vb, ok := b.([]any)
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if !jsonEqual(va[i], vb[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		vb, ok := b.(map[string]any)
		if !ok || len(va) != len(vb) {
			return false
		}
		for k, valA := range va {
			valB, exists := vb[k]
			if !exists || !jsonEqual(valA, valB) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}
