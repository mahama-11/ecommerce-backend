package imageruntime

import (
	"fmt"
	"strings"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/repository"
)

func (s *Service) buildCompiledPromptPlan(input CreateImageJobInput) (*compiledPromptPlan, error) {
	if strings.TrimSpace(input.PromptID) != "" {
		if strings.TrimSpace(input.Prompt) == "" {
			return nil, fmt.Errorf("compiled prompt snapshot is required")
		}
		return &compiledPromptPlan{
			ToolSlug:             normalizeToolSlug(input.SceneType),
			PromptStrategy:       "prompt_center_snapshot_v1",
			FinalPrompt:          strings.TrimSpace(input.Prompt),
			FinalNegativePrompt:  strings.TrimSpace(input.NegativePrompt),
			ResolvedTemplateCode: strings.TrimSpace(input.TemplateCode),
		}, nil
	}
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
	case "image_edit":
		return "image_edit"
	case "multi_image":
		return "multi_image"
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
