package promptcenter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

const SchemaVersion = "prompt.schema.v1"

type SourceAssetBinding struct {
	Slot    string `json:"slot"`
	AssetID string `json:"asset_id"`
}

type PreviewPromptInput struct {
	ProductID         string                 `json:"product_id" binding:"required"`
	SKUCode           string                 `json:"sku_code" binding:"required"`
	TemplateID        string                 `json:"template_id" binding:"required"`
	TemplateVersionID string                 `json:"template_version_id,omitempty"`
	TemplateCode      string                 `json:"template_code,omitempty"`
	ToolSlug          string                 `json:"tool_slug,omitempty"`
	SceneType         string                 `json:"scene_type" binding:"required"`
	Variables         map[string]any         `json:"variables,omitempty"`
	SourceAssets      []SourceAssetBinding   `json:"source_assets,omitempty"`
	IdempotencyKey    string                 `json:"idempotency_key,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type CompiledPrompt struct {
	Strategy            string          `json:"strategy"`
	FinalPrompt         string          `json:"final_prompt"`
	FinalNegativePrompt string          `json:"final_negative_prompt"`
	Sections            []PromptSection `json:"sections"`
}

type PromptSection struct {
	Header  string `json:"header"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationIssue `json:"errors"`
	Warnings []ValidationIssue `json:"warnings"`
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type PromptRunResponse struct {
	PromptID          string           `json:"prompt_id"`
	Status            string           `json:"status"`
	ProductID         string           `json:"product_id"`
	SKUCode           string           `json:"sku_code"`
	TemplateID        string           `json:"template_id"`
	TemplateVersionID string           `json:"template_version_id"`
	TemplateVersionNo int              `json:"template_version_no"`
	TemplateCode      string           `json:"template_code"`
	ToolSlug          string           `json:"tool_slug"`
	SceneType         string           `json:"scene_type"`
	SchemaVersion     string           `json:"schema_version"`
	ContentHash       string           `json:"content_hash"`
	SourceMapHash     string           `json:"source_map_hash"`
	Compiled          CompiledPrompt   `json:"compiled"`
	SourceMap         map[string]any   `json:"source_map"`
	Validation        ValidationResult `json:"validation"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type Service struct {
	repo         *repository.PromptCenterRepository
	templateRepo *repository.TemplateCenterRepository
	imageRepo    *repository.ImageRuntimeRepository
	productRepo  *repository.ProductCenterRepository
	appCfg       config.AppConfig
}

func NewService(repo *repository.PromptCenterRepository, templateRepo *repository.TemplateCenterRepository, imageRepo *repository.ImageRuntimeRepository, productRepo *repository.ProductCenterRepository, appCfg config.AppConfig) *Service {
	return &Service{repo: repo, templateRepo: templateRepo, imageRepo: imageRepo, productRepo: productRepo, appCfg: appCfg}
}

func (s *Service) Preview(userID, orgID string, input PreviewPromptInput) (*PromptRunResponse, error) {
	if s.repo == nil || s.templateRepo == nil || s.productRepo == nil || s.imageRepo == nil {
		return nil, fmt.Errorf("prompt center dependencies are required")
	}
	if existing, err := s.repo.FindPromptRunByIdempotencyKey(orgID, input.IdempotencyKey); err == nil {
		return mapPromptRunResponse(existing), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	product, err := s.requireBoundProduct(orgID, input.ProductID, input.SKUCode)
	if err != nil {
		return nil, err
	}
	detail, err := s.templateRepo.GetCatalogDetail(repository.Scope{OrgID: orgID, UserID: userID}, strings.TrimSpace(input.TemplateID), "zh")
	if err != nil {
		return nil, fmt.Errorf("template not found or not published")
	}
	if input.TemplateVersionID != "" && input.TemplateVersionID != detail.Version.ID {
		return nil, fmt.Errorf("template_version_id does not match current published template version")
	}
	toolSlug := firstNonEmpty(strings.TrimSpace(input.ToolSlug), detail.Catalog.ToolSlug, normalizeToolSlug(input.SceneType))
	validation := ValidationResult{Valid: true, Errors: []ValidationIssue{}, Warnings: []ValidationIssue{}}
	if detail.Catalog.ToolSlug != "" && toolSlug != detail.Catalog.ToolSlug {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationIssue{Code: "TOOL_TEMPLATE_MISMATCH", Field: "tool_slug", Message: "tool_slug does not match template binding"})
	}
	sourceBindings, sourceMap, validation, err := s.validateAndBuildSourceMap(orgID, product.ID, product.SKUCode, input, validation)
	if err != nil {
		return nil, err
	}
	compiled := s.compilePrompt(input, detail, toolSlug)
	sourceMap["variables"] = map[string]any{}
	for k := range input.Variables {
		sourceMap["variables"].(map[string]any)[k] = map[string]any{"source": "request.variables", "field": k}
	}
	payload := map[string]any{
		"schema_version":      SchemaVersion,
		"product_id":          product.ID,
		"sku_code":            product.SKUCode,
		"template_id":         detail.Catalog.ID,
		"template_version_id": detail.Version.ID,
		"template_code":       firstNonEmpty(detail.Catalog.ExternalCode, input.TemplateCode),
		"tool_slug":           toolSlug,
		"scene_type":          strings.TrimSpace(input.SceneType),
		"variables":           normalizeMap(input.Variables),
		"source_assets":       sourceBindings,
		"compiled":            compiled,
		"source_map":          sourceMap,
		"validation":          validation,
	}
	contentHash := hashStable(payload)
	sourceMapHash := hashStable(sourceMap)
	status := "validated"
	if !validation.Valid {
		status = "draft"
	}
	item := &models.EcommercePromptRun{
		ID:                      buildID("prompt"),
		OrganizationID:          orgID,
		UserID:                  userID,
		ProductID:               product.ID,
		SKUCode:                 product.SKUCode,
		TemplateID:              detail.Catalog.ID,
		TemplateVersionID:       detail.Version.ID,
		TemplateVersionNo:       detail.Version.VersionNo,
		TemplateCode:            firstNonEmpty(detail.Catalog.ExternalCode, input.TemplateCode),
		ToolSlug:                toolSlug,
		SceneType:               strings.TrimSpace(input.SceneType),
		Status:                  status,
		SchemaVersion:           SchemaVersion,
		ContentHash:             contentHash,
		SourceMapHash:           sourceMapHash,
		InputPayloadJSON:        mustMarshal(input),
		SourceAssetBindingsJSON: mustMarshal(sourceBindings),
		VariablesJSON:           mustMarshal(normalizeMap(input.Variables)),
		CompiledPromptJSON:      mustMarshal(compiled),
		SourceMapJSON:           mustMarshal(sourceMap),
		ValidationResultJSON:    mustMarshal(validation),
		IdempotencyKey:          strings.TrimSpace(input.IdempotencyKey),
	}
	if err := s.repo.CreatePromptRun(item); err != nil {
		return nil, err
	}
	return mapPromptRunResponse(item), nil
}

func (s *Service) Get(orgID, promptID string) (*PromptRunResponse, error) {
	item, err := s.repo.FindPromptRunByID(orgID, promptID)
	if err != nil {
		return nil, err
	}
	return mapPromptRunResponse(item), nil
}

func (s *Service) requireBoundProduct(orgID, productID, skuCode string) (*models.EcomProductSKU, error) {
	product, err := s.productRepo.GetProduct(repository.Scope{OrgID: orgID}, strings.TrimSpace(productID))
	if err != nil {
		return nil, fmt.Errorf("bound product not found")
	}
	if product.SKUCode != strings.TrimSpace(skuCode) {
		return nil, fmt.Errorf("sku_code does not match the selected product")
	}
	return product, nil
}

func (s *Service) validateAndBuildSourceMap(orgID, productID, skuCode string, input PreviewPromptInput, validation ValidationResult) ([]SourceAssetBinding, map[string]any, ValidationResult, error) {
	bindings := make([]SourceAssetBinding, 0, len(input.SourceAssets))
	sourceMap := map[string]any{"source_assets": map[string]any{}}
	for idx, binding := range input.SourceAssets {
		slot := firstNonEmpty(strings.TrimSpace(binding.Slot), fmt.Sprintf("source_%d", idx+1))
		assetID := strings.TrimSpace(binding.AssetID)
		if assetID == "" {
			validation.Valid = false
			validation.Errors = append(validation.Errors, ValidationIssue{Code: "SOURCE_ASSET_REQUIRED", Field: fmt.Sprintf("source_assets[%d].asset_id", idx), Message: "source asset id is required"})
			continue
		}
		asset, err := s.imageRepo.FindAssetByID(orgID, assetID)
		if err != nil {
			validation.Valid = false
			validation.Errors = append(validation.Errors, ValidationIssue{Code: "SOURCE_ASSET_NOT_FOUND", Field: fmt.Sprintf("source_assets[%d].asset_id", idx), Message: "source asset not found"})
			continue
		}
		meta := decodeMap(asset.Metadata)
		if stringValue(meta["product_id"]) != productID || stringValue(meta["sku_code"]) != skuCode {
			validation.Valid = false
			validation.Errors = append(validation.Errors, ValidationIssue{Code: "SOURCE_ASSET_PRODUCT_MISMATCH", Field: fmt.Sprintf("source_assets[%d].asset_id", idx), Message: "source asset is not bound to the selected product/SKU"})
		}
		bindings = append(bindings, SourceAssetBinding{Slot: slot, AssetID: asset.ID})
		sourceMap["source_assets"].(map[string]any)[slot] = map[string]any{"asset_id": asset.ID, "storage_key": asset.StorageKey, "product_id": productID, "sku_code": skuCode, "source": "ecommerce_assets"}
	}
	if len(input.SourceAssets) == 0 {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationIssue{Code: "SOURCE_ASSET_REQUIRED", Field: "source_assets", Message: "at least one source asset is required"})
	}
	return bindings, sourceMap, validation, nil
}

func (s *Service) compilePrompt(input PreviewPromptInput, detail *repository.CatalogDetail, toolSlug string) CompiledPrompt {
	variables := normalizeMap(input.Variables)
	userPrompt := firstNonEmpty(stringValue(variables["prompt"]), stringValue(variables["user_prompt"]), stringValue(variables["description"]))
	negativePrompt := stringValue(variables["negative_prompt"])
	policy := s.lookupScenePromptPolicy(input.SceneType)
	l1Source := "scene_policy"
	l1Prompt := policy.SystemPrompt
	if content := promptLayerContent(detail.Schema.PromptLayers, "l1"); content != "" {
		l1Prompt = content
		l1Source = "template_catalog"
	}
	l2Prompt := promptLayerContent(detail.Schema.PromptLayers, "l2")
	sections := []PromptSection{
		{Header: "[BUSINESS TOOL]", Source: "scene_policy", Content: firstNonEmpty(policy.DisplayName, toolSlug)},
		{Header: "[SYSTEM INSTRUCTION]", Source: l1Source, Content: l1Prompt},
	}
	if strings.TrimSpace(l2Prompt) != "" {
		sections = append(sections, PromptSection{Header: "[TEMPLATE STYLE]", Source: "template_catalog", Content: l2Prompt})
	}
	sections = append(sections, PromptSection{Header: "[USER CUSTOM]", Source: "request.variables.prompt", Content: userPrompt})
	return CompiledPrompt{Strategy: "business_layered_prompt_v1", FinalPrompt: joinPromptSections(sections), FinalNegativePrompt: joinNegativePrompts(s.globalNegativePrompt(), policy.DefaultNegativePrompt, negativePrompt), Sections: sections}
}

func (s *Service) lookupScenePromptPolicy(sceneType string) config.ScenePromptPolicyConfig {
	if policy, ok := s.appCfg.ImageRuntime.ScenePromptPolicies[normalizeSceneType(sceneType)]; ok {
		return policy
	}
	return config.ScenePromptPolicyConfig{ToolSlug: normalizeToolSlug(sceneType), DisplayName: firstNonEmpty(normalizeToolSlug(sceneType), "image-tool"), SystemPrompt: "你是一个专业的AI电商图像处理系统。任务目标：基于用户上传的商品或模特图片生成商业可用结果，优先保持主体身份、商品细节、品牌元素和电商发布质量。", DefaultNegativePrompt: "subject drift, brand detail loss, low commercial quality, unrealistic lighting"}
}

func (s *Service) globalNegativePrompt() string {
	return firstNonEmpty(strings.TrimSpace(s.appCfg.ImageRuntime.GlobalNegativePrompt), "blurry, noise, jpeg artifacts, watermark, text overlay, extra limbs, missing limbs, deformed anatomy")
}

func mapPromptRunResponse(item *models.EcommercePromptRun) *PromptRunResponse {
	compiled := CompiledPrompt{}
	_ = json.Unmarshal([]byte(item.CompiledPromptJSON), &compiled)
	validation := ValidationResult{}
	_ = json.Unmarshal([]byte(item.ValidationResultJSON), &validation)
	sourceMap := map[string]any{}
	_ = json.Unmarshal([]byte(item.SourceMapJSON), &sourceMap)
	return &PromptRunResponse{PromptID: item.ID, Status: item.Status, ProductID: item.ProductID, SKUCode: item.SKUCode, TemplateID: item.TemplateID, TemplateVersionID: item.TemplateVersionID, TemplateVersionNo: item.TemplateVersionNo, TemplateCode: item.TemplateCode, ToolSlug: item.ToolSlug, SceneType: item.SceneType, SchemaVersion: item.SchemaVersion, ContentHash: item.ContentHash, SourceMapHash: item.SourceMapHash, Compiled: compiled, SourceMap: sourceMap, Validation: validation, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func normalizeSceneType(sceneType string) string {
	return strings.ToLower(strings.TrimSpace(sceneType))
}
func normalizeToolSlug(sceneType string) string {
	return strings.ReplaceAll(normalizeSceneType(sceneType), "_", "-")
}

func promptLayerContent(layers map[string]any, layer string) string {
	raw, ok := layers[layer]
	if !ok {
		return ""
	}
	record, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue(record["content"]))
}

func joinPromptSections(sections []PromptSection) string {
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		content := strings.TrimSpace(section.Content)
		if content == "" {
			continue
		}
		if strings.TrimSpace(section.Header) != "" {
			parts = append(parts, section.Header+"\n"+content)
		} else {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func joinNegativePrompts(values ...string) string {
	seen := map[string]struct{}{}
	items := []string{}
	for _, value := range values {
		for _, token := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == '，' || r == '；' }) {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			items = append(items, token)
		}
	}
	return strings.Join(items, ", ")
}

func normalizeMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func hashStable(value any) string {
	body := mustMarshalStable(value)
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustMarshalStable(value any) string {
	normalized := normalizeForJSON(value)
	body, _ := json.Marshal(normalized)
	return string(body)
}

func normalizeForJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(v))
		for _, key := range keys {
			out[key] = normalizeForJSON(v[key])
		}
		return out
	case []SourceAssetBinding:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, map[string]any{"asset_id": item.AssetID, "slot": item.Slot})
		}
		return out
	case []PromptSection:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, map[string]any{"content": item.Content, "header": item.Header, "source": item.Source})
		}
		return out
	default:
		return v
	}
}

func buildID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }
func mustMarshal(value any) string { body, _ := json.Marshal(value); return string(body) }
func decodeMap(raw string) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
func stringValue(value any) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
