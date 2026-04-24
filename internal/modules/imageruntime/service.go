package imageruntime

import (
	"encoding/json"
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
)

type Service struct {
	repo         *repository.ImageRuntimeRepository
	templateRepo *repository.TemplateCenterRepository
	audit        *auditmodule.Service
	platform     *platform.Client
	appCfg       config.AppConfig
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
	FileName string         `json:"file_name"`
	MimeType string         `json:"mime_type" binding:"required"`
	Payload  string         `json:"payload" binding:"required"`
	Width    int            `json:"width"`
	Height   int            `json:"height"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type CreateImageJobInput struct {
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

func NewService(repo *repository.ImageRuntimeRepository, templateRepo *repository.TemplateCenterRepository, auditService *auditmodule.Service, platformClient *platform.Client, appCfg config.AppConfig) *Service {
	return &Service{repo: repo, templateRepo: templateRepo, audit: auditService, platform: platformClient, appCfg: appCfg}
}

func (s *Service) RegisterSourceAsset(userID, orgID string, input RegisterSourceAssetInput) (*AssetSummary, error) {
	if strings.TrimSpace(input.Payload) == "" {
		return nil, fmt.Errorf("source asset payload is required")
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
	sourceAsset, err := s.repo.FindAssetByID(orgID, input.SourceAssetID)
	if err != nil {
		return nil, err
	}
	promptPlan, err := s.buildCompiledPromptPlan(input)
	if err != nil {
		return nil, err
	}
	item := &models.EcommerceImageJob{
		ID:             buildID("job"),
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
		ProductCode:    s.productCode(),
		TaskType:       "image_generation",
		ProviderMode:   "async",
		OrganizationID: orgID,
		UserID:         userID,
		SourceType:     "ecommerce_image_job",
		SourceID:       item.ID,
		IdempotencyKey: idempotencyKey,
		InputManifest:  inputManifest,
		RouteSnapshot:  routeSnapshot,
		Metadata:       metadata,
		Priority:       100,
		MaxAttempts:    3,
		TimeoutSeconds: 600,
	})
	if err != nil {
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

func (s *Service) ListJobs(orgID, userID, sceneType string, limit int) ([]ImageJobSummary, error) {
	items, err := s.repo.ListJobs(orgID, userID, sceneType, limit)
	if err != nil {
		return nil, err
	}
	result := make([]ImageJobSummary, 0, len(items))
	for idx := range items {
		result = append(result, *mapImageJobSummary(&items[idx]))
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
	return item, nil
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

func stringValue(input any) string {
	value, _ := input.(string)
	return value
}

func copyMap(dst, src map[string]any) {
	maps.Copy(dst, src)
}
