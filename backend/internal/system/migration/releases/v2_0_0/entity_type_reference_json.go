// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"encoding/json"
	"fmt"
)

func transformInboundClientEntityTypeReferences(
	payload string,
	deploymentID string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (string, bool, error) {
	return transformJSONObject(payload, func(object map[string]interface{}) (bool, error) {
		changed, err := transformEntityTypeStringArray(
			object, "allowedUserTypes", deploymentID, "user", mapping, direction)
		if err != nil {
			return false, err
		}
		agentChanged, err := transformEntityTypeStringArray(
			object, "allowedAgentTypes", deploymentID, "agent", mapping, direction)
		if err != nil {
			return false, err
		}
		subjectChanged, err := transformEntityTypeMapKeys(
			object, "subjectAttribute", deploymentID, "user", mapping, direction)
		return changed || agentChanged || subjectChanged, err
	})
}

func transformIDPEntityTypeReferences(
	payload string,
	deploymentID string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (string, bool, error) {
	return transformJSONObject(payload, func(object map[string]interface{}) (bool, error) {
		changed := false
		if rawResolution, exists := object["userTypeResolution"]; exists && rawResolution != nil {
			resolution, ok := rawResolution.(map[string]interface{})
			if !ok {
				return false, fmt.Errorf("IDP userTypeResolution must be an object")
			}
			defaultChanged, err := transformEntityTypeString(
				resolution, "default", deploymentID, "user", mapping, direction)
			if err != nil {
				return false, err
			}
			changed = changed || defaultChanged
			if rawValueMapping, exists := resolution["valueMapping"]; exists && rawValueMapping != nil {
				valueMapping, ok := rawValueMapping.(map[string]interface{})
				if !ok {
					return false, fmt.Errorf("IDP userTypeResolution.valueMapping must be an object")
				}
				for key, rawValue := range valueMapping {
					value, ok := rawValue.(string)
					if !ok {
						return false, fmt.Errorf("IDP userTypeResolution.valueMapping[%q] must be a string", key)
					}
					translated, valueChanged, err := mapping.translate(
						deploymentID, "user", value, direction)
					if err != nil {
						return false, err
					}
					if valueChanged {
						valueMapping[key] = translated
						changed = true
					}
				}
			}
		}

		if rawMappings, exists := object["userTypeAttributeMappings"]; exists && rawMappings != nil {
			mappings, ok := rawMappings.([]interface{})
			if !ok {
				return false, fmt.Errorf("IDP userTypeAttributeMappings must be an array")
			}
			for index, rawMapping := range mappings {
				item, ok := rawMapping.(map[string]interface{})
				if !ok {
					return false, fmt.Errorf("IDP userTypeAttributeMappings[%d] must be an object", index)
				}
				itemChanged, err := transformEntityTypeString(
					item, "userType", deploymentID, "user", mapping, direction)
				if err != nil {
					return false, err
				}
				changed = changed || itemChanged
			}
		}
		return changed, nil
	})
}

func transformFlowEntityTypeReferences(
	payload string,
	deploymentID string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (string, bool, error) {
	if payload == "" {
		return payload, false, nil
	}
	var nodes []interface{}
	if err := json.Unmarshal([]byte(payload), &nodes); err != nil {
		return "", false, fmt.Errorf("decode flow nodes: %w", err)
	}
	changed := false
	for index, rawNode := range nodes {
		node, ok := rawNode.(map[string]interface{})
		if !ok {
			return "", false, fmt.Errorf("flow node %d must be an object", index)
		}
		rawExecutor, exists := node["executor"]
		if !exists || rawExecutor == nil {
			continue
		}
		executor, ok := rawExecutor.(map[string]interface{})
		if !ok {
			return "", false, fmt.Errorf("flow node %d executor must be an object", index)
		}
		name, _ := executor["name"].(string)
		var field, category string
		switch name {
		case "UserTypeResolver":
			field, category = "allowedUserTypes", "user"
		case "AgentTypeResolver":
			field, category = "allowedAgentTypes", "agent"
		default:
			continue
		}
		rawProperties, exists := node["properties"]
		if !exists || rawProperties == nil {
			continue
		}
		properties, ok := rawProperties.(map[string]interface{})
		if !ok {
			return "", false, fmt.Errorf("flow node %d properties must be an object", index)
		}
		nodeChanged, err := transformEntityTypeStringArray(
			properties, field, deploymentID, category, mapping, direction)
		if err != nil {
			return "", false, err
		}
		changed = changed || nodeChanged
	}
	if !changed {
		return payload, false, nil
	}
	updated, err := json.Marshal(nodes)
	if err != nil {
		return "", false, fmt.Errorf("encode flow nodes: %w", err)
	}
	return string(updated), true, nil
}

func transformJSONObject(
	payload string,
	transform func(map[string]interface{}) (bool, error),
) (string, bool, error) {
	if payload == "" {
		return payload, false, nil
	}
	var object map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &object); err != nil {
		return "", false, fmt.Errorf("decode JSON object: %w", err)
	}
	changed, err := transform(object)
	if err != nil {
		return "", false, err
	}
	if !changed {
		return payload, false, nil
	}
	updated, err := json.Marshal(object)
	if err != nil {
		return "", false, fmt.Errorf("encode JSON object: %w", err)
	}
	return string(updated), true, nil
}

func transformEntityTypeString(
	object map[string]interface{},
	field string,
	deploymentID string,
	category string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (bool, error) {
	rawValue, exists := object[field]
	if !exists || rawValue == nil {
		return false, nil
	}
	value, ok := rawValue.(string)
	if !ok {
		return false, fmt.Errorf("entity-type reference field %q must be a string", field)
	}
	translated, changed, err := mapping.translate(deploymentID, category, value, direction)
	if err != nil {
		return false, err
	}
	if changed {
		object[field] = translated
	}
	return changed, nil
}

func transformEntityTypeStringArray(
	object map[string]interface{},
	field string,
	deploymentID string,
	category string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (bool, error) {
	rawValues, exists := object[field]
	if !exists || rawValues == nil {
		return false, nil
	}
	values, ok := rawValues.([]interface{})
	if !ok {
		return false, fmt.Errorf("entity-type reference field %q must be an array", field)
	}
	changed := false
	for index, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok {
			return false, fmt.Errorf("entity-type reference field %q item %d must be a string", field, index)
		}
		translated, valueChanged, err := mapping.translate(deploymentID, category, value, direction)
		if err != nil {
			return false, err
		}
		if valueChanged {
			values[index] = translated
			changed = true
		}
	}
	return changed, nil
}

func transformEntityTypeMapKeys(
	object map[string]interface{},
	field string,
	deploymentID string,
	category string,
	mapping entityTypeReferenceMapping,
	direction entityTypeReferenceDirection,
) (bool, error) {
	rawValues, exists := object[field]
	if !exists || rawValues == nil {
		return false, nil
	}
	values, ok := rawValues.(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("entity-type reference field %q must be an object", field)
	}
	updated := make(map[string]interface{}, len(values))
	changed := false
	for key, value := range values {
		translated, keyChanged, err := mapping.translate(deploymentID, category, key, direction)
		if err != nil {
			return false, err
		}
		if _, duplicate := updated[translated]; duplicate {
			return false, fmt.Errorf("entity-type reference field %q produces duplicate key %q", field, translated)
		}
		updated[translated] = value
		changed = changed || keyChanged
	}
	if changed {
		object[field] = updated
	}
	return changed, nil
}
