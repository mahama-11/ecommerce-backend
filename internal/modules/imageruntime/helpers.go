package imageruntime

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"ecommerce-service/internal/models"
)

func buildID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func mergeJSON(raw string, incoming map[string]any) string {
	current := map[string]any{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &current)
	}
	if current == nil {
		current = map[string]any{}
	}
	copyMap(current, incoming)
	return mustMarshal(current)
}

func mustMarshal(value any) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func clampProgress(progress int, status string) int {
	if status == "completed" {
		return 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func mapResultStatusToStage(status string) string {
	switch status {
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	case "canceled":
		return "canceled"
	case "processing":
		return "provider_completed"
	default:
		return firstNonEmpty(status, "updated")
	}
}

func defaultStageMessage(stage, status string) string {
	switch stage {
	case "completed":
		return "Image job completed successfully"
	case "failed":
		return "Image job failed"
	case "canceled":
		return "Image job canceled"
	default:
		return firstNonEmpty(status, "Image job updated")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) productCode() string {
	return firstNonEmpty(s.appCfg.ProductCode, "ecommerce")
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		return 0
	}
}

func stringValue(value any) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(typed)
}

func stringValueFromMeta(raw string, key string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	values := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return ""
	}
	return stringValue(values[key])
}

func int64ValueFromMeta(raw string, key string) int64 {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	values := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return 0
	}
	return int64Value(values[key])
}

func defaultCFG(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	return value
}

func defaultDenoise(value float64) float64 {
	if value <= 0 {
		return 0.7
	}
	return value
}

func mapAssetSummary(item *models.EcommerceAsset) *AssetSummary {
	if item == nil {
		return nil
	}
	return &AssetSummary{
		ID:         item.ID,
		AssetType:  item.AssetType,
		SourceType: item.SourceType,
		StorageKey: item.StorageKey,
		MimeType:   item.MimeType,
		Width:      item.Width,
		Height:     item.Height,
		FileName:   item.FileName,
		Metadata:   decodeMap(item.Metadata),
	}
}

func mapImageJobSummary(item *models.EcommerceImageJob) *ImageJobSummary {
	if item == nil {
		return nil
	}
	return &ImageJobSummary{
		JobID:                 item.ID,
		OrganizationID:        item.OrganizationID,
		UserID:                item.UserID,
		SceneType:             item.SceneType,
		InputMode:             item.InputMode,
		SourceAssetID:         item.SourceAssetID,
		PromptID:              item.PromptID,
		RuntimeJobID:          item.RuntimeJobID,
		Status:                item.Status,
		Stage:                 item.Stage,
		StageMessage:          item.StageMessage,
		Progress:              item.Progress,
		ProviderJobID:         item.ProviderJobID,
		SelectedResultAssetID: item.SelectedResultAssetID,
		LastErrorCode:         item.LastErrorCode,
		LastErrorMessage:      item.LastErrorMessage,
		Metadata:              decodeMap(item.Metadata),
	}
}

func decodeMap(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func cloneMap(input map[string]any) map[string]any {
	out := map[string]any{}
	copyMap(out, input)
	return out
}

func copyMap(dst, src map[string]any) {
	maps.Copy(dst, src)
}
