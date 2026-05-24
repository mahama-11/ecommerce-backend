package imageruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
)

func (s *Service) prepareChargeContext(jobID, userID, orgID string, input CreateImageJobInput) (*chargeContext, error) {
	sceneCode := normalizeSceneCode(input.SceneType)
	return billinggate.New(s.platform).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionGeneration,
		SourceType:       billinggate.SourceTypeGenerationImageJob,
		SourceID:         jobID,
		ProductCode:      s.productCode(),
		OrganizationID:   orgID,
		UserID:           userID,
		BillableItemCode: s.billableItemCodeForScene(sceneCode),
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   strings.TrimSpace(input.IdempotencyKey),
		RouteSnapshot:    map[string]any{"scene_code": sceneCode, "input_mode": defaultInputMode(input.InputMode)},
		Metadata:         map[string]any{"scene_type": input.SceneType},
	})
}

func (s *Service) releaseChargeContext(chargeCtx *chargeContext, reason string) error {
	return billinggate.New(s.platform).Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: reason})
}

func chargeContextFromJob(item *models.EcommerceImageJob) *chargeContext {
	if item == nil {
		return nil
	}
	return &chargeContext{
		ChargeSessionID:  stringValueFromMeta(item.Metadata, "charge_session_id"),
		ReservationID:    stringValueFromMeta(item.Metadata, "reservation_id"),
		BillableItemCode: stringValueFromMeta(item.Metadata, "billable_item_code"),
		ResourceType:     stringValueFromMeta(item.Metadata, "resource_type"),
		UsageUnits:       int64ValueFromMeta(item.Metadata, "usage_units"),
	}
}

func (s *Service) bindChargeContext(item *models.EcommerceImageJob, chargeCtx *chargeContext, _ *platform.RuntimeJob) error {
	if chargeCtx == nil || item == nil {
		return nil
	}
	metadata := map[string]any{
		"charge_session_id":  chargeCtx.ChargeSessionID,
		"reservation_id":     chargeCtx.ReservationID,
		"billable_item_code": chargeCtx.BillableItemCode,
		"resource_type":      chargeCtx.ResourceType,
		"usage_units":        chargeCtx.UsageUnits,
	}
	item.Metadata = mergeJSON(item.Metadata, metadata)
	if chargeCtx.ChargeSessionID != "" {
		_ = billinggate.New(s.platform).MarkReserved(chargeCtx, map[string]any{"job_id": item.ID, "runtime_job_id": item.RuntimeJobID, "provider_job_id": item.ProviderJobID})
	}
	return nil
}

func (s *Service) billableItemCodeForScene(_ string) string {
	return "ecommerce.image.generate"
}

func normalizeSceneCode(sceneType string) string {
	switch normalizeSceneType(sceneType) {
	case "variation":
		return "variation"
	case "refinement":
		return "refinement"
	default:
		return "single"
	}
}

func (s *Service) findOrCreateResultAsset(item *models.EcommerceImageJob, input RecordJobResultsInput, variant RecordResultVariantInput) (*models.EcommerceAsset, error) {
	if existing, err := s.repo.FindGeneratedAssetByJobVariant(item.OrganizationID, item.ID, variant.Index); err == nil && existing != nil {
		return existing, nil
	}
	if strings.TrimSpace(variant.Asset.StorageKey) != "" {
		existing, err := s.repo.FindAssetByStorageKey(item.OrganizationID, variant.Asset.StorageKey)
		if err == nil && existing != nil {
			return existing, nil
		}
	}
	resultKey := fmt.Sprintf("%s:%d", item.ID, variant.Index)
	assetMetadata := map[string]any{
		"job_id":                item.ID,
		"generation_task_id":    item.ID,
		"variant_index":         variant.Index,
		"result_index":          variant.Index,
		"generation_result_key": resultKey,
		"variant_status":        variant.Status,
		"is_selected":           variant.IsSelected,
	}
	jobMetadata := decodeMap(item.Metadata)
	if contract, ok := jobMetadata["prompt_contract"].(map[string]any); ok && stringValue(contract["prompt_id"]) != "" {
		assetMetadata["prompt_id"] = stringValue(contract["prompt_id"])
		assetMetadata["prompt_content_hash"] = stringValue(contract["content_hash"])
		assetMetadata["template_id"] = stringValue(contract["template_id"])
		assetMetadata["template_version_id"] = stringValue(contract["template_version_id"])
	}
	if len(input.Metadata) > 0 {
		assetMetadata["runtime_metadata"] = input.Metadata
	}
	asset := &models.EcommerceAsset{
		ID:             buildID("asset"),
		OrganizationID: item.OrganizationID,
		UserID:         item.UserID,
		AssetType:      firstNonEmpty(variant.Asset.AssetType, "generated"),
		SourceType:     firstNonEmpty(variant.Asset.SourceType, "generated"),
		StorageKey:     variant.Asset.StorageKey,
		MimeType:       variant.Asset.MimeType,
		Width:          variant.Asset.Width,
		Height:         variant.Asset.Height,
		FileName:       variant.Asset.FileName,
		Metadata:       mustMarshal(assetMetadata),
	}
	if err := s.repo.CreateAsset(asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Service) finalizeChargeForJob(item *models.EcommerceImageJob, status string) error {
	if s.platform == nil || item == nil {
		return nil
	}
	meta := map[string]any{}
	if strings.TrimSpace(item.Metadata) != "" {
		_ = json.Unmarshal([]byte(item.Metadata), &meta)
	}
	chargeSessionID := stringValue(meta["charge_session_id"])
	reservationID := stringValue(meta["reservation_id"])
	billableItemCode := firstNonEmpty(stringValue(meta["billable_item_code"]), "ecommerce.image.generate")
	usageUnits := int64Value(meta["usage_units"])
	if usageUnits <= 0 {
		usageUnits = 1
	}
	if chargeSessionID == "" {
		return nil
	}
	chargeCtx := &chargeContext{
		Action:           billinggate.ActionGeneration,
		SourceType:       billinggate.SourceTypeGenerationImageJob,
		SourceID:         item.ID,
		ProductCode:      s.productCode(),
		OrganizationID:   item.OrganizationID,
		UserID:           item.UserID,
		ChargeSessionID:  chargeSessionID,
		ReservationID:    reservationID,
		BillableItemCode: billableItemCode,
		ResourceType:     firstNonEmpty(stringValue(meta["resource_type"]), billinggate.DefaultResourceType),
		UsageUnits:       usageUnits,
	}
	if status == "completed" {
		eventID := fmt.Sprintf("evt_%s", item.ID)
		commit, err := billinggate.New(s.platform).Commit(billinggate.CommitInput{
			Context:      chargeCtx,
			SourceAction: normalizeSceneCode(item.SceneType),
			EventID:      eventID,
			Dimensions:   map[string]any{"scene_code": normalizeSceneCode(item.SceneType)},
			Metadata:     map[string]any{"job_id": item.ID, "charge_session_id": chargeSessionID},
		})
		if err != nil {
			return err
		}
		_ = s.persistBillingCharge(item, eventID, billableItemCode, usageUnits, commit.Result)
		return nil
	}
	return billinggate.New(s.platform).Release(billinggate.ReleaseInput{Context: chargeCtx, Metadata: map[string]any{"job_status": status}})
}

func (s *Service) persistBillingCharge(item *models.EcommerceImageJob, eventID, billableItemCode string, usageUnits int64, result *platform.FinalizeResult) error {
	if s.commercialRepo == nil || item == nil || result == nil || result.Settlement == nil {
		return nil
	}
	record := &models.BillingChargeRecord{
		ID:               buildID("charge"),
		ProductCode:      s.productCode(),
		OrganizationID:   item.OrganizationID,
		UserID:           item.UserID,
		EventID:          eventID,
		BusinessType:     "image_runtime_generation",
		SceneCode:        normalizeSceneCode(item.SceneType),
		SourceType:       "ecommerce_image_job",
		SourceID:         item.ID,
		BillableItemCode: billableItemCode,
		ChargeMode:       "runtime_metering",
		ChargeSessionID:  stringValueFromMeta(item.Metadata, "charge_session_id"),
		SettlementID:     result.Settlement.EventID,
		Currency:         result.Settlement.Currency,
		NetAmount:        result.Settlement.Amount,
		QuotaConsumed:    usageUnits,
		Status:           firstNonEmpty(result.Settlement.Status, "settled"),
		OccurredAt:       time.Now().UTC(),
		MetadataJSON:     mustMarshal(map[string]any{"usage_units": usageUnits, "job_id": item.ID}),
		ChannelStatus:    "pending",
	}
	return s.commercialRepo.CreateBillingChargeRecord(record)
}
