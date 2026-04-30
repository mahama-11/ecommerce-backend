package imageruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

type Service struct {
	repo           *repository.ImageRuntimeRepository
	commercialRepo *repository.CommercialRepository
	templateRepo   *repository.TemplateCenterRepository
	productRepo    *repository.ProductCenterRepository
	audit          *auditmodule.Service
	platform       *platform.Client
	appCfg         config.AppConfig
}

type UpdateJobRuntimeInput struct {
	Status        string         `json:"status"`
	Stage         string         `json:"stage"`
	StageMessage  string         `json:"stage_message"`
	Progress      *int           `json:"progress"`
	EtaSeconds    *int           `json:"eta_seconds"`
	ProviderJobID string         `json:"provider_job_id"`
	Metadata      map[string]any `json:"metadata"`
}

type RecordResultAssetInput struct {
	AssetType  string `json:"asset_type"`
	SourceType string `json:"source_type"`
	FileName   string `json:"file_name,omitempty"`
	StorageKey string `json:"storage_key,omitempty"`
	SourceURL  string `json:"source_url"`
	PreviewURL string `json:"preview_url,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

type RecordResultVariantInput struct {
	Index      int                    `json:"index" binding:"required"`
	Status     string                 `json:"status" binding:"required"`
	IsSelected bool                   `json:"is_selected,omitempty"`
	Asset      RecordResultAssetInput `json:"asset"`
}

type RecordJobResultsInput struct {
	Status       string                     `json:"status" binding:"required"`
	Progress     int                        `json:"progress"`
	StageMessage string                     `json:"stage_message,omitempty"`
	ErrorCode    string                     `json:"error_code,omitempty"`
	ErrorMessage string                     `json:"error_message,omitempty"`
	Metadata     map[string]any             `json:"metadata,omitempty"`
	Variants     []RecordResultVariantInput `json:"variants"`
}

type RegisterSourceAssetInput struct {
	ProductID string         `json:"product_id" binding:"required"`
	SKUCode   string         `json:"sku_code" binding:"required"`
	FileName  string         `json:"file_name"`
	MimeType  string         `json:"mime_type" binding:"required"`
	Payload   string         `json:"payload" binding:"required"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type CreateImageJobInput struct {
	ProductID          string   `json:"product_id" binding:"required"`
	SKUCode            string   `json:"sku_code" binding:"required"`
	SceneType          string   `json:"scene_type" binding:"required"`
	InputMode          string   `json:"input_mode,omitempty"`
	SourceAssetID      string   `json:"source_asset_id" binding:"required"`
	Prompt             string   `json:"prompt" binding:"required"`
	NegativePrompt     string   `json:"negative_prompt,omitempty"`
	Objective          string   `json:"objective,omitempty"`
	PreferredProviders []string `json:"preferred_providers,omitempty"`
	RequestedVariants  int      `json:"requested_variants,omitempty"`
	Width              int      `json:"width,omitempty"`
	Height             int      `json:"height,omitempty"`
	Steps              int      `json:"steps,omitempty"`
	CFG                float64  `json:"cfg,omitempty"`
	Denoise            float64  `json:"denoise,omitempty"`
	TemplateCode       string   `json:"template_code,omitempty"`
	IdempotencyKey     string   `json:"idempotency_key,omitempty"`
}

type AssetSummary struct {
	ID         string         `json:"id"`
	AssetType  string         `json:"asset_type"`
	SourceType string         `json:"source_type"`
	StorageKey string         `json:"storage_key"`
	MimeType   string         `json:"mime_type"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	FileName   string         `json:"file_name"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ImageJobSummary struct {
	JobID                 string         `json:"job_id"`
	OrganizationID        string         `json:"organization_id"`
	UserID                string         `json:"user_id"`
	SceneType             string         `json:"scene_type"`
	InputMode             string         `json:"input_mode"`
	SourceAssetID         string         `json:"source_asset_id"`
	RuntimeJobID          string         `json:"runtime_job_id"`
	Status                string         `json:"status"`
	Stage                 string         `json:"stage"`
	StageMessage          string         `json:"stage_message"`
	Progress              int            `json:"progress"`
	ProviderJobID         string         `json:"provider_job_id,omitempty"`
	SelectedResultAssetID string         `json:"selected_result_asset_id,omitempty"`
	LastErrorCode         string         `json:"last_error_code,omitempty"`
	LastErrorMessage      string         `json:"last_error_message,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type compiledPromptPlan struct {
	ToolSlug             string
	PromptStrategy       string
	FinalPrompt          string
	FinalNegativePrompt  string
	ResolvedTemplateID   string
	ResolvedTemplateCode string
	ResolvedTemplateName string
	L1Source             string
	L2Enabled            bool
}

type chargeContext struct {
	ChargeSessionID  string
	ReservationID    string
	BillableItemCode string
	ResourceType     string
	UsageUnits       int64
}

func NewService(repo *repository.ImageRuntimeRepository, commercialRepo *repository.CommercialRepository, templateRepo *repository.TemplateCenterRepository, productRepo *repository.ProductCenterRepository, auditService *auditmodule.Service, platformClient *platform.Client, appCfg config.AppConfig) *Service {
	return &Service{repo: repo, commercialRepo: commercialRepo, templateRepo: templateRepo, productRepo: productRepo, audit: auditService, platform: platformClient, appCfg: appCfg}
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
	item.Status = firstNonEmpty(runtimeJob.Status, "queued")
	item.Stage = firstNonEmpty(runtimeJob.Stage, "queued")
	item.StageMessage = firstNonEmpty(runtimeJob.StageMessage, "Runtime job queued")
	item.ProviderJobID = runtimeJob.ProviderJobID
	if err := s.bindChargeContext(item, chargeCtx, runtimeJob); err != nil {
		return nil, err
	}
	if err := s.repo.SaveJob(item); err != nil {
		return nil, err
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

func (s *Service) buildCompiledPromptPlan(input CreateImageJobInput) (*compiledPromptPlan, error) {
	userPrompt := strings.TrimSpace(input.Prompt)
	if userPrompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	sceneType := normalizeSceneType(input.SceneType)
	policy := s.lookupScenePromptPolicy(sceneType)
	expectedToolSlug := normalizeToolSlug(sceneType)
	template, err := s.resolveRuntimePromptTemplate(strings.TrimSpace(input.TemplateCode), expectedToolSlug)
	if err != nil {
		return nil, err
	}

	l1Source := "scene_policy"
	l1Prompt := policy.SystemPrompt
	if content := promptLayerContent(template, "l1"); content != "" {
		l1Prompt = content
		l1Source = "template_catalog"
	}
	l2Prompt := promptLayerContent(template, "l2")
	finalPrompt := joinPromptSections(
		promptSection{Header: "[BUSINESS TOOL]", Content: firstNonEmpty(policy.DisplayName, expectedToolSlug)},
		promptSection{Header: "[SYSTEM INSTRUCTION]", Content: l1Prompt},
		promptSection{Header: "[TEMPLATE STYLE]", Content: l2Prompt},
		promptSection{Header: "[USER CUSTOM]", Content: userPrompt},
	)

	return &compiledPromptPlan{
		ToolSlug:             firstNonEmpty(policy.ToolSlug, expectedToolSlug),
		PromptStrategy:       "business_layered_prompt_v1",
		FinalPrompt:          finalPrompt,
		FinalNegativePrompt:  joinNegativePrompts(s.globalNegativePrompt(), policy.DefaultNegativePrompt, input.NegativePrompt),
		ResolvedTemplateID:   stringValueFromTemplate(template, func(item *repository.RuntimePromptTemplate) string { return item.TemplateID }),
		ResolvedTemplateCode: stringValueFromTemplate(template, func(item *repository.RuntimePromptTemplate) string { return item.ExternalCode }),
		ResolvedTemplateName: stringValueFromTemplate(template, func(item *repository.RuntimePromptTemplate) string { return item.Name }),
		L1Source:             l1Source,
		L2Enabled:            strings.TrimSpace(l2Prompt) != "",
	}, nil
}

func (s *Service) resolveRuntimePromptTemplate(templateRef, expectedToolSlug string) (*repository.RuntimePromptTemplate, error) {
	if s.templateRepo == nil || templateRef == "" {
		return nil, nil
	}
	template, err := s.templateRepo.ResolveRuntimePromptTemplate(templateRef)
	if err != nil || template == nil {
		return template, err
	}
	if expectedToolSlug != "" && strings.TrimSpace(template.ToolSlug) != "" && template.ToolSlug != expectedToolSlug {
		return nil, nil
	}
	return template, nil
}

func (s *Service) lookupScenePromptPolicy(sceneType string) config.ScenePromptPolicyConfig {
	policies := s.appCfg.ImageRuntime.ScenePromptPolicies
	if policy, ok := policies[normalizeSceneType(sceneType)]; ok {
		return policy
	}
	return config.ScenePromptPolicyConfig{
		ToolSlug:              normalizeToolSlug(sceneType),
		DisplayName:           firstNonEmpty(normalizeToolSlug(sceneType), "image-tool"),
		SystemPrompt:          "你是一个专业的AI电商图像处理系统。任务目标：基于用户上传的商品或模特图片生成商业可用结果，优先保持主体身份、商品细节、品牌元素和电商发布质量。",
		DefaultNegativePrompt: "subject drift, brand detail loss, low commercial quality, unrealistic lighting",
	}
}

func (s *Service) globalNegativePrompt() string {
	return firstNonEmpty(
		strings.TrimSpace(s.appCfg.ImageRuntime.GlobalNegativePrompt),
		"blurry, noise, jpeg artifacts, watermark, text overlay, extra limbs, missing limbs, deformed anatomy, disfigured, bad proportions, duplicate objects, floating objects with no shadow, unrealistic lighting inconsistency, oversaturated colors, artificial plastic texture, lowres, draft quality, sketch, illustration style",
	)
}

type promptSection struct {
	Header  string
	Content string
}

func joinPromptSections(sections ...promptSection) string {
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		content := strings.TrimSpace(section.Content)
		if content == "" {
			continue
		}
		header := strings.TrimSpace(section.Header)
		if header != "" {
			parts = append(parts, header+"\n"+content)
			continue
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

func joinNegativePrompts(values ...string) string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		for _, token := range splitPromptTerms(value) {
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			items = append(items, token)
		}
	}
	return strings.Join(items, ", ")
}

func splitPromptTerms(value string) []string {
	tokens := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '\n', ';', '，', '；':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func promptLayerContent(template *repository.RuntimePromptTemplate, layer string) string {
	if template == nil || len(template.PromptLayers) == 0 {
		return ""
	}
	raw, ok := template.PromptLayers[layer]
	if !ok {
		return ""
	}
	record, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue(record["content"]))
}

func stringValueFromTemplate(template *repository.RuntimePromptTemplate, getter func(*repository.RuntimePromptTemplate) string) string {
	if template == nil || getter == nil {
		return ""
	}
	return strings.TrimSpace(getter(template))
}

func normalizeSceneType(sceneType string) string {
	return strings.ToLower(strings.TrimSpace(sceneType))
}

func normalizeToolSlug(sceneType string) string {
	return strings.ReplaceAll(normalizeSceneType(sceneType), "_", "-")
}

func defaultInputMode(inputMode string) string {
	switch strings.TrimSpace(inputMode) {
	case "text_to_image":
		return "text_to_image"
	default:
		return "image_to_image"
	}
}

func normalizeObjective(objective string) string {
	switch strings.TrimSpace(objective) {
	case "speed", "cost", "balanced":
		return strings.TrimSpace(objective)
	default:
		return "quality"
	}
}

func defaultRequestedVariants(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func defaultSteps(value int) int {
	if value <= 0 {
		return 8
	}
	return value
}

func defaultDimension(value int) int {
	if value <= 0 {
		return 1024
	}
	return value
}

func (s *Service) prepareChargeContext(jobID, userID, orgID string, input CreateImageJobInput) (*chargeContext, error) {
	if s.platform == nil {
		return &chargeContext{}, nil
	}
	sceneCode := normalizeSceneCode(input.SceneType)
	billableItemCode := s.billableItemCodeForScene(sceneCode)
	resourceType := "quota"
	usageUnits := int64(1)
	session, err := s.platform.CreateChargeSession(platform.CreateChargeSessionInput{
		SourceType:         "ecommerce_image_job",
		SourceID:           jobID,
		ProductCode:        s.productCode(),
		OrganizationID:     orgID,
		UserID:             userID,
		BillingSubjectType: "organization",
		BillingSubjectID:   orgID,
		BillableItemCode:   billableItemCode,
		ResourceType:       resourceType,
		ReservationKey:     firstNonEmpty(strings.TrimSpace(input.IdempotencyKey), fmt.Sprintf("reserve:%s", jobID)),
		EstimatedUnits:     usageUnits,
		RouteSnapshot:      mustMarshal(map[string]any{"scene_code": sceneCode, "input_mode": defaultInputMode(input.InputMode)}),
		Metadata:           mustMarshal(map[string]any{"scene_type": input.SceneType}),
	})
	if err != nil {
		return nil, err
	}
	reservation, reserveErr := s.platform.ReserveResources(platform.ReserveInput{
		ResourceType:       resourceType,
		BillingSubjectType: "organization",
		BillingSubjectID:   orgID,
		BillableItemCode:   billableItemCode,
		ReservationKey:     fmt.Sprintf("reserve:%s", jobID),
		ReferenceID:        session.ID,
		Units:              usageUnits,
		Metadata:           mustMarshal(map[string]any{"job_id": jobID, "charge_session_id": session.ID}),
	})
	if reserveErr != nil {
		return nil, reserveErr
	}
	if reservation == nil || strings.TrimSpace(reservation.ID) == "" {
		return nil, fmt.Errorf("resource reservation missing for job %s", jobID)
	}
	return &chargeContext{
		ChargeSessionID:  session.ID,
		ReservationID:    reservation.ID,
		BillableItemCode: billableItemCode,
		ResourceType:     resourceType,
		UsageUnits:       usageUnits,
	}, nil
}

func (s *Service) releaseChargeContext(chargeCtx *chargeContext, reason string) error {
	if s.platform == nil || chargeCtx == nil {
		return nil
	}
	if chargeCtx.ReservationID != "" {
		_, _ = s.platform.ReleaseReservation(chargeCtx.ReservationID)
	}
	if chargeCtx.ChargeSessionID != "" {
		_, _ = s.platform.UpdateChargeSession(chargeCtx.ChargeSessionID, platform.UpdateChargeSessionInput{
			Status:        "released",
			ReservationID: chargeCtx.ReservationID,
			Metadata:      mustMarshal(map[string]any{"release_reason": reason}),
		})
	}
	return nil
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
		_, _ = s.platform.UpdateChargeSession(chargeCtx.ChargeSessionID, platform.UpdateChargeSessionInput{
			Status:        "reserved",
			ReservationID: chargeCtx.ReservationID,
			Metadata:      mustMarshal(map[string]any{"job_id": item.ID, "runtime_job_id": item.RuntimeJobID, "provider_job_id": item.ProviderJobID}),
		})
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
	if strings.TrimSpace(variant.Asset.StorageKey) != "" {
		existing, err := s.repo.FindAssetByStorageKey(item.OrganizationID, variant.Asset.StorageKey)
		if err == nil && existing != nil {
			return existing, nil
		}
	}
	assetMetadata := map[string]any{
		"job_id":         item.ID,
		"variant_index":  variant.Index,
		"variant_status": variant.Status,
		"is_selected":    variant.IsSelected,
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
	if status == "completed" {
		eventID := fmt.Sprintf("evt_%s", item.ID)
		result, err := s.platform.FinalizeMetering(platform.FinalizeInput{
			FinalizationID: fmt.Sprintf("fin_%s", item.ID),
			ReservationID:  reservationID,
			IngestEventInput: platform.IngestEventInput{
				EventID:            eventID,
				SourceType:         "ecommerce_image_job",
				SourceID:           item.ID,
				SourceAction:       normalizeSceneCode(item.SceneType),
				ProductCode:        s.productCode(),
				OrgID:              item.OrganizationID,
				UserID:             item.UserID,
				BillableItemCode:   billableItemCode,
				ChargeGroupID:      item.ID,
				BillingSubjectType: "organization",
				BillingSubjectID:   item.OrganizationID,
				UsageUnits:         usageUnits,
				Unit:               "action",
				OccurredAt:         time.Now().UTC().Format(time.RFC3339),
				Dimensions:         mustMarshal(map[string]any{"scene_code": normalizeSceneCode(item.SceneType)}),
				Metadata:           mustMarshal(map[string]any{"job_id": item.ID, "charge_session_id": chargeSessionID}),
			},
		})
		if err != nil {
			return err
		}
		finalUnits := usageUnits
		_, _ = s.platform.UpdateChargeSession(chargeSessionID, platform.UpdateChargeSessionInput{
			Status:        "settled",
			ReservationID: reservationID,
			EventID:       eventID,
			SettlementID:  result.Settlement.ID,
			FinalUnits:    &finalUnits,
			Metadata:      mustMarshal(map[string]any{"event_id": eventID}),
		})
		_ = s.persistBillingCharge(item, eventID, billableItemCode, usageUnits, result)
		return nil
	}
	if reservationID != "" {
		_, _ = s.platform.ReleaseReservation(reservationID)
	}
	_, _ = s.platform.UpdateChargeSession(chargeSessionID, platform.UpdateChargeSessionInput{
		Status:        "released",
		ReservationID: reservationID,
		Metadata:      mustMarshal(map[string]any{"job_status": status}),
	})
	return nil
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
