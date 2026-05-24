package imageruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

type Service struct {
	repo           *repository.ImageRuntimeRepository
	commercialRepo *repository.CommercialRepository
	templateRepo   *repository.TemplateCenterRepository
	promptRepo     *repository.PromptCenterRepository
	productRepo    *repository.ProductCenterRepository
	audit          *auditmodule.Service
	platform       *platform.Client
	appCfg         config.AppConfig
}

func NewService(repo *repository.ImageRuntimeRepository, commercialRepo *repository.CommercialRepository, templateRepo *repository.TemplateCenterRepository, productRepo *repository.ProductCenterRepository, auditService *auditmodule.Service, platformClient *platform.Client, appCfg config.AppConfig) *Service {
	return &Service{repo: repo, commercialRepo: commercialRepo, templateRepo: templateRepo, productRepo: productRepo, audit: auditService, platform: platformClient, appCfg: appCfg}
}

func (s *Service) WithPromptRepository(promptRepo *repository.PromptCenterRepository) *Service {
	s.promptRepo = promptRepo
	return s
}

func (s *Service) RegisterSourceAsset(userID, orgID string, input RegisterSourceAssetInput) (*AssetSummary, error) {
	if strings.TrimSpace(input.Payload) == "" {
		return nil, fmt.Errorf("source asset payload is required")
	}
	product, err := s.requireBoundProduct(orgID, input.ProductID, input.SKUCode)
	if err != nil {
		return nil, err
	}
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	stored, err := s.platform.UploadAsset(platform.UploadAssetInput{
		ProductCode: s.productCode(),
		Category:    "ecommerce-assets",
		FileName:    input.FileName,
		MimeType:    input.MimeType,
		Payload:     input.Payload,
	})
	if err != nil {
		return nil, err
	}
	metadata := cloneMap(input.Metadata)
	metadata["kind"] = "source"
	metadata["product_id"] = product.ID
	metadata["sku_code"] = product.SKUCode
	asset := &models.EcommerceAsset{
		ID:             buildID("asset"),
		OrganizationID: orgID,
		UserID:         userID,
		AssetType:      "source",
		SourceType:     "upload",
		StorageKey:     stored.StorageKey,
		MimeType:       firstNonEmpty(stored.MimeType, input.MimeType),
		Width:          input.Width,
		Height:         input.Height,
		FileName:       input.FileName,
		Metadata:       mustMarshal(metadata),
	}
	if err := s.repo.CreateAsset(asset); err != nil {
		return nil, err
	}
	return mapAssetSummary(asset), nil
}

func (s *Service) CreateImageJob(userID, orgID string, input CreateImageJobInput) (*ImageJobSummary, error) {
	product, err := s.requireBoundProduct(orgID, input.ProductID, input.SKUCode)
	if err != nil {
		return nil, err
	}
	promptContract, err := s.resolvePromptContract(orgID, product, &input)
	if err != nil {
		return nil, err
	}
	sourceAsset, err := s.repo.FindAssetByID(orgID, input.SourceAssetID)
	if err != nil {
		return nil, err
	}
	if err := validateSourceAssetBinding(sourceAsset, product.ID); err != nil {
		return nil, err
	}
	promptPlan, err := s.buildCompiledPromptPlan(input)
	if err != nil {
		return nil, err
	}
	jobID := buildID("job")
	chargeCtx, err := s.prepareChargeContext(jobID, userID, orgID, input)
	if err != nil {
		return nil, err
	}
	item := &models.EcommerceImageJob{
		ID:             jobID,
		OrganizationID: orgID,
		UserID:         userID,
		SceneType:      strings.TrimSpace(input.SceneType),
		InputMode:      defaultInputMode(input.InputMode),
		SourceAssetID:  sourceAsset.ID,
		PromptID:       strings.TrimSpace(input.PromptID),
		Status:         "queued",
		Stage:          "queued",
		StageMessage:   "Image job created and waiting for runtime dispatch",
		Progress:       0,
		Metadata: mustMarshal(map[string]any{
			"prompt":                  strings.TrimSpace(input.Prompt),
			"negative_prompt":         strings.TrimSpace(input.NegativePrompt),
			"final_prompt":            promptPlan.FinalPrompt,
			"final_negative_prompt":   promptPlan.FinalNegativePrompt,
			"objective":               normalizeObjective(input.Objective),
			"preferred_providers":     input.PreferredProviders,
			"template_code":           input.TemplateCode,
			"resolved_template_id":    promptPlan.ResolvedTemplateID,
			"resolved_template_code":  promptPlan.ResolvedTemplateCode,
			"resolved_template_name":  promptPlan.ResolvedTemplateName,
			"tool_slug":               promptPlan.ToolSlug,
			"prompt_strategy":         promptPlan.PromptStrategy,
			"prompt_layer_l1_source":  promptPlan.L1Source,
			"prompt_layer_l2_enabled": promptPlan.L2Enabled,
			"product_id":              product.ID,
			"sku_code":                product.SKUCode,
			"charge_session_id":       chargeCtx.ChargeSessionID,
			"reservation_id":          chargeCtx.ReservationID,
			"billable_item_code":      chargeCtx.BillableItemCode,
			"resource_type":           chargeCtx.ResourceType,
			"usage_units":             chargeCtx.UsageUnits,
			"prompt_contract":         promptContract,
		}),
	}
	err = s.repo.CreateJob(item)
	if err != nil {
		return nil, err
	}

	inputManifest := mustMarshal(map[string]any{
		"input_mode": item.InputMode,
		"params_snapshot": map[string]any{
			"prompt":          promptPlan.FinalPrompt,
			"negative_prompt": promptPlan.FinalNegativePrompt,
			"steps":           defaultSteps(input.Steps),
			"cfg":             defaultCFG(input.CFG),
			"denoise":         defaultDenoise(input.Denoise),
			"width":           defaultDimension(input.Width),
			"height":          defaultDimension(input.Height),
		},
		"source_asset_ids": []string{sourceAsset.ID},
		"source_assets": []map[string]any{{
			"id":          sourceAsset.ID,
			"storage_key": sourceAsset.StorageKey,
			"mime_type":   sourceAsset.MimeType,
			"width":       sourceAsset.Width,
			"height":      sourceAsset.Height,
			"product_id":  product.ID,
			"sku_code":    product.SKUCode,
		}},
		"requested_variants": defaultRequestedVariants(input.RequestedVariants),
		"prompt_contract":    promptContract,
	})
	routeSnapshot := mustMarshal(map[string]any{
		"objective":           normalizeObjective(input.Objective),
		"preferred_providers": input.PreferredProviders,
	})
	metadata := mustMarshal(map[string]any{
		"scene_type":             item.SceneType,
		"template_code":          input.TemplateCode,
		"resolved_template_id":   promptPlan.ResolvedTemplateID,
		"resolved_template_code": promptPlan.ResolvedTemplateCode,
		"resolved_template_name": promptPlan.ResolvedTemplateName,
		"tool_slug":              promptPlan.ToolSlug,
		"prompt_strategy":        promptPlan.PromptStrategy,
		"prompt_contract":        promptContract,
	})
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("ecommerce:%s:create_runtime", item.ID)
	}
	runtimeJob, err := s.platform.CreateRuntimeJob(platform.CreateRuntimeJobInput{
		ProductCode:     s.productCode(),
		TaskType:        "image_generation",
		ProviderMode:    "async",
		OrganizationID:  orgID,
		UserID:          userID,
		SourceType:      "ecommerce_image_job",
		SourceID:        item.ID,
		IdempotencyKey:  idempotencyKey,
		ChargeSessionID: chargeCtx.ChargeSessionID,
		InputManifest:   inputManifest,
		RouteSnapshot:   routeSnapshot,
		Metadata:        metadata,
		Priority:        100,
		MaxAttempts:     3,
		TimeoutSeconds:  600,
	})
	if err != nil {
		_ = s.releaseChargeContext(chargeCtx, "runtime_create_failed")
		item.Status = "failed"
		item.Stage = "runtime_create_failed"
		item.StageMessage = "Failed to create runtime job"
		item.LastErrorCode = "RUNTIME_CREATE_FAILED"
		item.LastErrorMessage = err.Error()
		_ = s.repo.SaveJob(item)
		return nil, err
	}
	item.RuntimeJobID = runtimeJob.ID
	item.ProviderJobID = runtimeJob.ProviderJobID
	if latest, findErr := s.repo.FindJobByID(item.ID); findErr == nil && latest != nil {
		item = latest
		item.RuntimeJobID = runtimeJob.ID
		item.ProviderJobID = firstNonEmpty(runtimeJob.ProviderJobID, item.ProviderJobID)
	}
	if err := s.bindChargeContext(item, chargeCtx, runtimeJob); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateJobRuntimeBinding(item.ID, item.RuntimeJobID, item.ProviderJobID, item.Metadata); err != nil {
		return nil, err
	}
	if latest, findErr := s.repo.FindJobByID(item.ID); findErr == nil && latest != nil {
		return mapImageJobSummary(latest), nil
	}
	return mapImageJobSummary(item), nil
}

func (s *Service) GetJob(orgID, jobID string) (*ImageJobSummary, error) {
	item, err := s.repo.FindJobByID(jobID)
	if err != nil {
		return nil, err
	}
	if orgID != "" && item.OrganizationID != orgID {
		return nil, fmt.Errorf("image job not found")
	}
	return mapImageJobSummary(item), nil
}

func (s *Service) CancelJob(orgID, jobID string) (*models.EcommerceImageJob, error) {
	item, err := s.repo.FindJobByID(jobID)
	if err != nil {
		return nil, err
	}
	if orgID != "" && item.OrganizationID != orgID {
		return nil, fmt.Errorf("image job not found")
	}
	if item.Status == "completed" || item.Status == "failed" || item.Status == "canceled" {
		return item, nil
	}
	now := time.Now()
	if s.platform != nil && strings.TrimSpace(item.RuntimeJobID) != "" {
		if _, cancelErr := s.platform.CancelRuntimeJob(item.RuntimeJobID); cancelErr != nil && !platform.IsNotFound(cancelErr) {
			item.Metadata = mergeJSON(item.Metadata, map[string]any{
				"cancel_runtime_error": cancelErr.Error(),
			})
		}
	}
	item.Status = "canceled"
	item.Stage = "canceled"
	item.StageMessage = "Image job canceled by user"
	item.Progress = clampProgress(item.Progress, "canceled")
	item.CanceledAt = &now
	if err := s.repo.SaveJob(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) ListJobs(orgID, userID, sceneType, productID string, limit int) ([]ImageJobSummary, error) {
	items, err := s.repo.ListJobs(orgID, userID, sceneType, limit)
	if err != nil {
		return nil, err
	}
	result := make([]ImageJobSummary, 0, len(items))
	for idx := range items {
		summary := mapImageJobSummary(&items[idx])
		if productID != "" && stringValue(summary.Metadata["product_id"]) != productID {
			continue
		}
		result = append(result, *summary)
	}
	return result, nil
}

func (s *Service) UpdateJobRuntime(jobID string, input UpdateJobRuntimeInput) (*models.EcommerceImageJob, error) {
	item, err := s.repo.FindJobByID(jobID)
	if err != nil {
		return nil, err
	}
	if input.Status != "" {
		item.Status = input.Status
	}
	if input.Stage != "" {
		item.Stage = input.Stage
	}
	if input.StageMessage != "" {
		item.StageMessage = input.StageMessage
	}
	if input.Progress != nil {
		item.Progress = clampProgress(*input.Progress, item.Status)
	}
	if input.ProviderJobID != "" {
		item.ProviderJobID = input.ProviderJobID
	}
	if len(input.Metadata) > 0 {
		item.Metadata = mergeJSON(item.Metadata, input.Metadata)
	}
	if input.ErrorCode != "" {
		item.LastErrorCode = input.ErrorCode
	}
	if input.ErrorMessage != "" {
		item.LastErrorMessage = input.ErrorMessage
	}
	now := time.Now()
	switch item.Status {
	case "completed":
		item.CompletedAt = &now
		item.CanceledAt = nil
	case "canceled":
		item.CanceledAt = &now
	case "failed":
		if item.Stage == "" {
			item.Stage = "failed"
		}
		if item.LastErrorMessage == "" {
			item.LastErrorMessage = item.StageMessage
		}
		_ = s.releaseChargeContext(chargeContextFromJob(item), "runtime_failed")
	}
	if err := s.repo.SaveJob(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) RecordJobResults(jobID string, input RecordJobResultsInput) (*models.EcommerceImageJob, error) {
	item, err := s.repo.FindJobByID(jobID)
	if err != nil {
		return nil, err
	}
	item.Status = input.Status
	item.Progress = clampProgress(input.Progress, input.Status)
	item.Stage = mapResultStatusToStage(input.Status)
	item.StageMessage = firstNonEmpty(strings.TrimSpace(input.StageMessage), defaultStageMessage(item.Stage, input.Status))
	item.LastErrorCode = input.ErrorCode
	item.LastErrorMessage = input.ErrorMessage
	if len(input.Metadata) > 0 {
		item.Metadata = mergeJSON(item.Metadata, input.Metadata)
	}
	now := time.Now()
	if input.Status == "completed" {
		item.CompletedAt = &now
	}
	if input.Status == "canceled" {
		item.CanceledAt = &now
	}

	selectedAssetID := ""
	for idx, variant := range input.Variants {
		asset, assetErr := s.findOrCreateResultAsset(item, input, variant)
		if assetErr != nil {
			return nil, assetErr
		}
		if bindErr := s.archiveGeneratedAssetToProduct(item, asset, variant); bindErr != nil {
			return nil, bindErr
		}
		if variant.IsSelected || (selectedAssetID == "" && idx == 0) {
			selectedAssetID = asset.ID
		}
	}
	if selectedAssetID != "" {
		item.SelectedResultAssetID = selectedAssetID
	}
	if err := s.repo.SaveJob(item); err != nil {
		return nil, err
	}
	if err := s.finalizeChargeForJob(item, input.Status); err != nil {
		item.Metadata = mergeJSON(item.Metadata, map[string]any{
			"metering_status": "failed",
			"metering_error":  err.Error(),
		})
		_ = s.repo.SaveJob(item)
	}
	return item, nil
}

func (s *Service) requireBoundProduct(orgID, productID, skuCode string) (*models.EcomProductSKU, error) {
	if s.productRepo == nil {
		return nil, fmt.Errorf("product repository is required")
	}
	productID = strings.TrimSpace(productID)
	skuCode = strings.TrimSpace(skuCode)
	if productID == "" || skuCode == "" {
		return nil, fmt.Errorf("product_id and sku_code are required")
	}
	product, err := s.productRepo.GetProduct(repository.Scope{OrgID: orgID}, productID)
	if err != nil {
		return nil, fmt.Errorf("bound product not found")
	}
	if product.SKUCode != skuCode {
		return nil, fmt.Errorf("sku_code does not match the selected product")
	}
	return product, nil
}

func validateSourceAssetBinding(asset *models.EcommerceAsset, productID string) error {
	if asset == nil {
		return fmt.Errorf("source asset is required")
	}
	if productID == "" {
		return fmt.Errorf("product_id is required")
	}
	meta := decodeMap(asset.Metadata)
	if stringValue(meta["product_id"]) != productID {
		return fmt.Errorf("source asset is not bound to the selected product")
	}
	return nil
}

func (s *Service) resolvePromptContract(orgID string, product *models.EcomProductSKU, input *CreateImageJobInput) (map[string]any, error) {
	promptID := strings.TrimSpace(input.PromptID)
	if promptID == "" {
		return map[string]any{"mode": "legacy"}, nil
	}
	if s.promptRepo == nil {
		return nil, fmt.Errorf("prompt repository is required")
	}
	promptRun, err := s.promptRepo.FindPromptRunByID(orgID, promptID)
	if err != nil {
		return nil, fmt.Errorf("prompt not found")
	}
	if promptRun.ProductID != product.ID || promptRun.SKUCode != product.SKUCode {
		return nil, fmt.Errorf("prompt does not match selected product/SKU")
	}
	if promptRun.Status != "validated" && promptRun.Status != "bound" && promptRun.Status != "executed" {
		return nil, fmt.Errorf("prompt is not validated")
	}
	var compiled promptcenter.CompiledPrompt
	if err := json.Unmarshal([]byte(promptRun.CompiledPromptJSON), &compiled); err != nil {
		return nil, fmt.Errorf("prompt compiled snapshot is invalid")
	}
	var bindings []promptcenter.SourceAssetBinding
	_ = json.Unmarshal([]byte(promptRun.SourceAssetBindingsJSON), &bindings)
	if strings.TrimSpace(input.SourceAssetID) == "" && len(bindings) > 0 {
		input.SourceAssetID = bindings[0].AssetID
	}
	if strings.TrimSpace(input.SourceAssetID) == "" {
		return nil, fmt.Errorf("source_asset_id is required")
	}
	matched := len(bindings) == 0
	for _, binding := range bindings {
		if binding.AssetID == strings.TrimSpace(input.SourceAssetID) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("source asset does not match prompt source bindings")
	}
	input.Prompt = compiled.FinalPrompt
	input.NegativePrompt = compiled.FinalNegativePrompt
	input.TemplateCode = firstNonEmpty(input.TemplateCode, promptRun.TemplateCode)
	return map[string]any{
		"mode":                "prompt_id",
		"prompt_id":           promptRun.ID,
		"template_id":         promptRun.TemplateID,
		"template_version_id": promptRun.TemplateVersionID,
		"template_code":       promptRun.TemplateCode,
		"schema_version":      promptRun.SchemaVersion,
		"content_hash":        promptRun.ContentHash,
		"source_map_hash":     promptRun.SourceMapHash,
	}, nil
}

func (s *Service) archiveGeneratedAssetToProduct(job *models.EcommerceImageJob, asset *models.EcommerceAsset, variant RecordResultVariantInput) error {
	if s.productRepo == nil || job == nil || asset == nil {
		return nil
	}
	meta := decodeMap(job.Metadata)
	productID := stringValue(meta["product_id"])
	if productID == "" {
		return nil
	}
	scope := repository.Scope{OrgID: job.OrganizationID, UserID: job.UserID}
	if _, err := s.productRepo.GetProduct(scope, productID); err != nil {
		return err
	}
	if _, err := s.productRepo.FindProductAssetRelation(scope, productID, asset.ID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	existingAssets, err := s.productRepo.ListProductAssets(scope, productID)
	if err != nil {
		return err
	}
	item := models.EcomAssetRelation{
		ID:           buildID("rel"),
		AssetID:      asset.ID,
		OwnerType:    models.AssetRelationOwnerTypeProduct,
		OwnerID:      productID,
		RelationType: models.AssetRelationTypeResult,
		AssetRole:    assetRoleForScene(job.SceneType),
		IsPrimary:    variant.IsSelected && !hasPrimaryProductAsset(existingAssets),
		SortOrder:    variant.Index,
	}
	if _, err := s.productRepo.AddProductAsset(scope, item); err != nil {
		return err
	}
	if _, err := s.productRepo.CreateProductActivity(scope, models.EcomProductActivity{
		ID:        buildID("activity"),
		ProductID: productID,
		Type:      models.ProductActivityTypeAssetCreated,
		Title:     "AI Result Archived",
		Summary:   fmt.Sprintf("Generated asset archived from image job %s", job.ID),
		Metadata: mustMarshal(map[string]any{
			"job_id":     job.ID,
			"asset_id":   asset.ID,
			"scene_type": job.SceneType,
		}),
	}); err != nil {
		return err
	}
	return s.syncProductStatuses(scope, productID)
}

func (s *Service) syncProductStatuses(scope repository.Scope, productID string) error {
	product, err := s.productRepo.GetProduct(scope, productID)
	if err != nil {
		return err
	}
	assets, err := s.productRepo.ListProductAssets(scope, productID)
	if err != nil {
		return err
	}
	switch {
	case len(assets) == 0:
		product.AssetStatus = models.AssetStatusMissing
	case hasAssetRole(assets, models.AssetRoleHero) && hasAssetRole(assets, models.AssetRoleModelShot):
		product.AssetStatus = models.AssetStatusReady
	default:
		product.AssetStatus = models.AssetStatusPartial
	}
	if product.Status != models.ProductStatusPublished && product.Status != models.ProductStatusArchived {
		switch {
		case product.AssetStatus == models.AssetStatusReady &&
			product.ListingStatus == models.ListingStatusReady &&
			product.ExportStatus == models.ExportStatusReady:
			product.Status = models.ProductStatusExportReady
		case product.AssetStatus == models.AssetStatusReady &&
			product.ListingStatus == models.ListingStatusReady:
			product.Status = models.ProductStatusListingReady
		case product.AssetStatus == models.AssetStatusReady:
			product.Status = models.ProductStatusAssetsReady
		default:
			product.Status = models.ProductStatusDraft
		}
	}
	_, err = s.productRepo.UpdateProduct(scope, *product)
	return err
}

func hasPrimaryProductAsset(items []models.EcomAssetRelation) bool {
	for _, item := range items {
		if item.IsPrimary {
			return true
		}
	}
	return false
}

func hasAssetRole(items []models.EcomAssetRelation, role string) bool {
	for _, item := range items {
		if item.AssetRole == role {
			return true
		}
	}
	return false
}

func assetRoleForScene(sceneType string) string {
	switch normalizeSceneType(sceneType) {
	case "ai_posture", "model_pose", "refinement":
		return models.AssetRoleModelShot
	case "scene", "scene_composition", "background_swap":
		return models.AssetRoleSceneShot
	case "detail", "detail_focus":
		return models.AssetRoleDetailShot
	default:
		return models.AssetRoleHero
	}
}

func (s *Service) GetAssetContent(orgID, assetID string) (*models.EcommerceAsset, io.ReadCloser, http.Header, error) {
	var (
		item *models.EcommerceAsset
		err  error
	)
	if orgID != "" {
		item, err = s.repo.FindAssetByID(orgID, assetID)
	} else {
		item, err = s.repo.FindAssetByIDGlobal(assetID)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(item.StorageKey) == "" {
		return item, nil, nil, fmt.Errorf("asset storage key is empty")
	}
	if s.platform == nil {
		return item, nil, nil, fmt.Errorf("platform client is required")
	}
	body, headers, err := s.platform.DownloadAsset(item.StorageKey)
	if err != nil {
		return item, nil, nil, err
	}
	return item, body, headers, nil
}
