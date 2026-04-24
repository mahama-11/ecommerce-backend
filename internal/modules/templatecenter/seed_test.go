package templatecenter

import (
	"encoding/json"
	"testing"
)

func TestGeneratedSeedImageAssetRequirementsStayInSync(t *testing.T) {
	definitions := seedCatalogs()
	if len(definitions) == 0 {
		t.Fatal("seed catalogs should not be empty")
	}

	for _, item := range definitions {
		inputSchema := decodeMap(t, item.Schema.InputSchemaJSON)
		defaultVariables := decodeMap(t, item.Schema.DefaultVariablesJSON)

		imageFields := collectImageSchemaKeys(inputSchema["fields"])
		imageAssetRequirements := collectImageAssetRequirementSlots(defaultVariables["assetRequirements"])

		for key := range imageFields {
			if _, ok := imageAssetRequirements[key]; !ok {
				t.Fatalf("template %s has image field %q without matching asset requirement", item.Catalog.Slug, key)
			}
		}
		for slot := range imageAssetRequirements {
			if _, ok := imageFields[slot]; !ok {
				t.Fatalf("template %s has image asset requirement %q without matching input field", item.Catalog.Slug, slot)
			}
		}
	}
}

func collectImageSchemaKeys(raw any) map[string]struct{} {
	items, _ := raw.([]any)
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		record, _ := item.(map[string]any)
		fieldType, _ := record["type"].(string)
		key, _ := record["key"].(string)
		if key == "" || fieldType == "" {
			continue
		}
		if fieldType == "image" || fieldType == "image[]" {
			result[key] = struct{}{}
		}
	}
	return result
}

func collectImageAssetRequirementSlots(raw any) map[string]struct{} {
	items, _ := raw.([]any)
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		record, _ := item.(map[string]any)
		fieldType, _ := record["fieldType"].(string)
		slot, _ := record["slot"].(string)
		if slot == "" || fieldType == "" {
			continue
		}
		if fieldType == "image" || fieldType == "image[]" {
			result[slot] = struct{}{}
		}
	}
	return result
}

func decodeMap(t *testing.T, payload string) map[string]any {
	t.Helper()
	if payload == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("decode map: %v", err)
	}
	return out
}
