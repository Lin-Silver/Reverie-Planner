package main

import (
	"encoding/json"
	"fmt"
)

func validateDBFilterTree(raw json.RawMessage, depth int) error {
	if depth > 8 {
		return fmt.Errorf("filter nesting exceeds limit")
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil || node == nil {
		return fmt.Errorf("filter node must be a JSON object")
	}
	var kind string
	if err := json.Unmarshal(node["kind"], &kind); err != nil {
		return fmt.Errorf("filter kind required")
	}
	switch kind {
	case "group":
		var join string
		if err := json.Unmarshal(node["join"], &join); err != nil || (join != "and" && join != "or") {
			return fmt.Errorf("filter join must be and/or")
		}
		var children []json.RawMessage
		if err := json.Unmarshal(node["children"], &children); err != nil || children == nil {
			return fmt.Errorf("filter children must be an array")
		}
		if len(children) > 40 {
			return fmt.Errorf("too many filter conditions")
		}
		for _, child := range children {
			if err := validateDBFilterTree(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	case "rule":
		var prop, operator string
		if err := json.Unmarshal(node["propertyId"], &prop); err != nil || prop == "" {
			return fmt.Errorf("filter property required")
		}
		if err := json.Unmarshal(node["operator"], &operator); err != nil {
			return fmt.Errorf("filter operator required")
		}
		valid := map[string]bool{"equals": true, "not_equals": true, "contains": true, "not_contains": true, "gt": true, "gte": true, "lt": true, "lte": true, "before": true, "after": true, "is_empty": true, "is_not_empty": true}
		if !valid[operator] {
			return fmt.Errorf("unsupported filter operator")
		}
		if operator != "is_empty" && operator != "is_not_empty" {
			var val string
			if err := json.Unmarshal(node["value"], &val); err != nil {
				return fmt.Errorf("filter comparison value must be a string")
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported filter node")
	}
}

// Validate the settings consumed directly by React. JSON syntax alone does not
// prevent persisted objects/nulls from crashing text inputs and table layouts.
// Unknown fields remain allowed for forward-compatible layout extensions.
func validateDBPresentationConfig(body map[string]json.RawMessage) error {
	for _, key := range []string{"filter", "sort", "groupBy", "dateProperty", "galleryCoverProperty"} {
		if raw, exists := body[key]; exists {
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil || value == nil {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	for _, key := range []string{"columnOrder", "hiddenColumns", "galleryProperties"} {
		if raw, exists := body[key]; exists {
			var ids []string
			if err := json.Unmarshal(raw, &ids); err != nil || ids == nil || len(ids) > 200 {
				return fmt.Errorf("%s must be an array of at most 200 property IDs", key)
			}
			seen := map[string]bool{}
			for _, id := range ids {
				if id == "" || len(id) > 128 || seen[id] {
					return fmt.Errorf("%s property IDs must be nonempty and unique", key)
				}
				seen[id] = true
			}
		}
	}
	if raw, exists := body["columnWidths"]; exists {
		var widths map[string]float64
		if err := json.Unmarshal(raw, &widths); err != nil || widths == nil || len(widths) > 200 {
			return fmt.Errorf("columnWidths must be an object of at most 200 widths")
		}
		for id, width := range widths {
			if id == "" || len(id) > 128 || width < 100 || width > 700 {
				return fmt.Errorf("columnWidths requires property IDs and widths between 100 and 700")
			}
		}
	}
	if raw, exists := body["filterTree"]; exists {
		var root struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &root); err != nil || root.Kind != "group" {
			return fmt.Errorf("filterTree must be a filter group")
		}
		if err := validateDBFilterTree(raw, 0); err != nil {
			return err
		}
	}
	return nil
}
