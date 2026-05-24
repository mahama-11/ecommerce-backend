package visualworkflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/promptcenter"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
)

func TestCreateStrategyReportJobCreatesPlatformTextRuntime(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_strategy", "org_strategy", "SKU-S")
	session, err := service.CreateSession("user_strategy", "org_strategy", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyStrategyReportMatrix(), runtimeJob: &platform.RuntimeJob{ID: "runtime-strategy-1", ProductCode: "ecommerce", TaskType: "strategy_report", Status: "queued", Stage: "queued"}}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateStrategyReportJob("org_strategy", session.ID, CreateStrategyReportJobRequest{Marketplace: "amazon", Locale: "en-US", ReportGoal: "positioning", SourceFacts: map[string]any{"competitor_count": 3}, IdempotencyKey: "strategy-1"})
	if err != nil {
		t.Fatalf("create strategy report: %v", err)
	}
	if resp.RuntimeJobID != "runtime-strategy-1" || len(fake.createInputs) != 1 {
		t.Fatalf("expected strategy runtime, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	input := fake.createInputs[0]
	if input.TaskType != "strategy_report" || input.SourceType != "visual_strategy_report" || input.SourceID != session.ID {
		t.Fatalf("unexpected strategy runtime input: %+v", input)
	}
	if strings.Contains(input.InputManifest, "storage_key") || strings.Contains(input.InputManifest, "provider_job_id") {
		t.Fatalf("strategy manifest leaked execution metadata: %s", input.InputManifest)
	}
}

func TestStrategyReportResultPersistsSanitizedMetadata(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_strategy_result", "org_strategy_result", "SKU-SR")
	session, err := service.CreateSession("user_strategy_result", "org_strategy_result", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	report := map[string]any{"schema_version": "ecommerce_strategy_report.v1", "status": "ready", "summary": "Premium positioning", "recommendations": []string{"lead with material"}}
	result, err := service.InternalRecordStrategyReportResults(session.ID, InternalRecordResultsRequest{Status: "completed", Progress: 100, Stage: "completed", Variants: []map[string]any{{"inline_data": toJSONForTest(report)}}})
	if err != nil {
		t.Fatalf("record strategy report: %v", err)
	}
	metadata := result.Metadata
	strategy := metadata["strategy_report"].(map[string]any)
	if strategy["status"] != "completed" {
		t.Fatalf("strategy report not completed in metadata: %+v", strategy)
	}
	persisted := strategy["report"].(map[string]any)
	if persisted["summary"] != "Premium positioning" {
		t.Fatalf("strategy report summary not persisted: %+v", persisted)
	}
}

func TestCreateGenerationFanoutCreatesMatrixRuntimeJobs(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout", "org_fanout", "SKU-F")
	assets := []models.EcommerceAsset{
		{ID: "asset_fanout_1", OrganizationID: "org_fanout", UserID: "user_fanout", AssetType: "image", SourceType: "upload", StorageKey: "store/a.png", MimeType: "image/png", FileName: "a.png", Width: 1024, Height: 1024, Metadata: "{}"},
		{ID: "asset_fanout_2", OrganizationID: "org_fanout", UserID: "user_fanout", AssetType: "image", SourceType: "upload", StorageKey: "store/b.png", MimeType: "image/png", FileName: "b.png", Width: 1024, Height: 1024, Metadata: "{}"},
	}
	for i := range assets {
		if err := db.Create(&assets[i]).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout", "org_fanout", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_fanout", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_fanout_1", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout", session.ID, CreateGenerationFanoutRequest{IdempotencyKey: "fanout-1", SourceAssetIDs: []string{"asset_fanout_1", "asset_fanout_2"}, TemplateIDs: []string{"tpl_a", "tpl_b"}, RequestedVariants: 1, ProviderConfig: map[string]any{"resolution_id": "1024-square"}})
	if err != nil {
		t.Fatalf("create fanout: %v", err)
	}
	if len(resp.Items) != 4 || len(fake.createInputs) != 4 {
		t.Fatalf("expected 4 fanout items/runtime jobs, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	seen := map[string]bool{}
	for _, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		if params["fanout_id"] != "fanout-1" || params["template_id"] == "" || params["source_asset_id"] == "" {
			t.Fatalf("fanout params missing from manifest: %#v", params)
		}
		seen[fmt.Sprint(params["source_asset_id"])+":"+fmt.Sprint(params["template_id"])] = true
		sourceIDs, _ := manifest["source_asset_ids"].([]any)
		if len(sourceIDs) != 1 || fmt.Sprint(sourceIDs[0]) != fmt.Sprint(params["source_asset_id"]) {
			t.Fatalf("fanout runtime should use exactly its selected source asset: manifest=%#v params=%#v", manifest["source_asset_ids"], params)
		}
		if strings.Contains(input.InputManifest, "provider_job_id") {
			t.Fatalf("fanout manifest leaked execution artifact: %s", input.InputManifest)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("fanout matrix did not cover all source/template pairs: %#v", seen)
	}
}

func TestCreateGenerationFanoutAllowsStoredSourceAssetWithUnknownDimensions(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_unknown_dims", "org_fanout_unknown_dims", "SKU-FU")
	asset := models.EcommerceAsset{ID: "asset_unknown_dims", OrganizationID: "org_fanout_unknown_dims", UserID: "user_fanout_unknown_dims", AssetType: "image", SourceType: "upload", StorageKey: "store/unknown.png", MimeType: "image/png", FileName: "unknown.png", Width: 0, Height: 0, Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	session, err := service.CreateSession("user_fanout_unknown_dims", "org_fanout_unknown_dims", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_unknown_dims", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_unknown_dims", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_unknown_dims", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout_unknown_dims", session.ID, CreateGenerationFanoutRequest{
		TemplateSlots:     []GenerationFanoutTemplateSlotRequest{{SourceAssetID: "asset_unknown_dims", TemplateID: "tpl_a", SceneTag: "主图"}},
		RequestedVariants: 1,
	})
	if err != nil {
		t.Fatalf("create fanout with unknown dimensions should pass: %v", err)
	}
	if len(resp.Items) != 1 || len(fake.createInputs) != 1 {
		t.Fatalf("expected one runtime job for unknown dimension asset, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
}

func TestPromptComposerFromFixedQuestionsPersistsSections(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_prompt_composer", "org_prompt_composer", "SKU-PC")
	session, err := service.CreateSession("user_prompt_composer", "org_prompt_composer", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_prompt_composer", "org_prompt_composer", session.ID, src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_pc_sku_product", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "product", ElementKey: "sku_product", Label: "SKU 梳子主体", ValueJSON: toJSONForTest(map[string]any{"description": "木柄宽齿梳主体，保留完整轮廓"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_pc_sku_bg", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "background", ElementKey: "sku_background", Label: "SKU 白底", ValueJSON: toJSONForTest(map[string]any{"description": "普通白色拍摄背景"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_pc_ref_product", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "product", ElementKey: "reference_product", Label: "参考香薰瓶", ValueJSON: toJSONForTest(map[string]any{"description": "参考图里的香薰瓶不要进入画面"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
		{ID: "vde_pc_ref_bg", OrganizationID: "org_prompt_composer", SessionID: session.ID, JobID: "vdj_pc", ElementType: "background", ElementKey: "reference_background", Label: "参考木质浴室", ValueJSON: toJSONForTest(map[string]any{"description": "暖色木质浴室台面与柔和自然光"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	_, err = service.ApplyAttentionTree("org_prompt_composer", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_pc_sku_product", Decision: "keep", Question: "要不要 SKU 产品？", Answer: "yes", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "vde_pc_sku_bg", Decision: "drop", Question: "要不要 SKU 背景？", Answer: "no", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background"}},
		{ElementID: "vde_pc_ref_product", Decision: "drop", Question: "要不要参考素材产品？", Answer: "no", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product"}},
		{ElementID: "vde_pc_ref_bg", Decision: "keep", Question: "要不要参考素材背景？", Answer: "yes", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}, DriftControls: map[string]any{"sku_weight": 30, "reference_weight": 70}})
	if err != nil {
		t.Fatalf("apply fixed question decisions: %v", err)
	}
	model, err := service.repo.GetSession("org_prompt_composer", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"出图目标", "保留 SKU 商品主体完整清晰", "只更换或重建背景", "SKU 解析结果", "参考素材解析结果", "四问选择", "侧重配置", "必须避免", "SKU 梳子主体", "参考木质浴室", "不要使用 SKU 原图背景", "不要把参考素材中的产品", "侧重参考素材 70%"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("composed prompt missing %q: %s", want, finalPrompt)
		}
	}
	sections, _ := plan.Variables["prompt_sections"].([]any)
	if len(sections) < 6 {
		t.Fatalf("expected prompt_sections to persist objective/facts/choices/negative parts, got %#v", plan.Variables["prompt_sections"])
	}
	if negative, _ := plan.Variables["negative_prompt_text"].(string); !strings.Contains(negative, "不要保留 SKU 原图背景") || !strings.Contains(negative, "不要把参考素材中的产品") {
		t.Fatalf("expected purpose-specific negative prompt text, got %q", negative)
	}
}

func TestBuildIntentFusionInputManifestKeepsNonEmptyNeedsReviewReferenceAnalysis(t *testing.T) {
	manifest := buildIntentFusionInputManifest(
		[]IntentSourceReferenceDTO{{SourceReferenceID: "vsr_ref", Role: "reference"}, {SourceReferenceID: "vsr_sku", Role: "sku"}},
		[]IntentElementDTO{{ElementID: "fixed:reference_background", Decision: "keep", Label: "采用参考背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}}},
		[]models.EcommerceVisualDeconstructionElement{
			{ID: "vde_sku", ElementType: "product_fact", ElementKey: "visual_description", Label: "Visual description", Confidence: 0.95, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "purple earbuds"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
			{ID: "vde_ref", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "minimalist product photography against white background with clean lines and soft shadows"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference", "source_reference_id": "vsr_ref"})},
		},
		nil,
	)
	entries := manifestEntries(manifest["reference_strategies"])
	if len(entries) != 1 {
		t.Fatalf("expected one reference strategy, got %#v", manifest["reference_strategies"])
	}
	text := promptComposerAnalysisText("参考素材解析结果", entries)
	if !strings.Contains(text, "minimalist product photography against white background with clean lines and soft shadows") {
		t.Fatalf("expected real weak reference analysis in prompt text, got %q from %#v", text, entries)
	}
	if strings.Contains(text, "按当前参考素材直接生成出图方案") {
		t.Fatalf("non-empty reference analysis should not be replaced by fallback: %q", text)
	}
}

func TestPromptComposerIncludesWeakReferenceAnalysisWhenUserKeepsReferenceStyle(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_reference_prompt", "org_reference_prompt", "SKU-RP")
	session, err := service.CreateSession("user_reference_prompt", "org_reference_prompt", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_reference_prompt", "org_reference_prompt", session.ID, src); err != nil {
			t.Fatalf("create source reference: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_rp_sku", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "product_fact", ElementKey: "visual_description", Label: "Visual description", Confidence: 0.95, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "purple earbuds with charging case"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_rp_ref_product", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "product_fact", ElementKey: "product_info", Label: "图片中的产品信息", Confidence: 0.8, Readiness: "ready", ValueJSON: toJSONForTest(map[string]any{"description": "一款黑色头戴式耳机，带麦克风支架和线缆。"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
		{ID: "vde_rp_ref", OrganizationID: "org_reference_prompt", SessionID: session.ID, JobID: "vdj_rp", ElementType: "reference_strategy", ElementKey: "style", Label: "Reference style", Confidence: 0, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": "minimalist product photography against white background with clean lines and soft shadows"}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	_, err = service.UpdateSession("org_reference_prompt", session.ID, UpdateSessionRequest{IntentSpec: &IntentSpecDTO{
		SchemaVersion: "visual_intent_spec.v1",
		Selections: []IntentElementDTO{
			{ElementID: "fixed:sku_product", Decision: "keep", Label: "保留 SKU 原图产品", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product", "source_element_id": "vde_rp_sku"}},
			{ElementID: "fixed:sku_background", Decision: "drop", Label: "保留 SKU 原图背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background", "source_element_id": "vde_rp_sku"}},
			{ElementID: "fixed:reference_product", Decision: "drop", Label: "使用参考素材产品", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product", "source_element_id": "vde_rp_ref_product"}},
			{ElementID: "fixed:reference_background", Decision: "keep", Label: "采用参考素材背景", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background", "source_element_id": "vde_rp_ref"}},
		},
	}})
	if err == nil {
		err = service.refreshIntentInputManifest("org_reference_prompt", session.ID, nil, nil)
	}
	if err != nil {
		t.Fatalf("apply fixed choices: %v", err)
	}
	model, err := service.repo.GetSession("org_reference_prompt", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	if strings.Contains(finalPrompt, "minimalist product photography against white background with clean lines and soft shadows") {

	} else {
		t.Fatalf("reference analysis should enter composed prompt, got: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "一款黑色头戴式耳机") {
		t.Fatalf("dropped reference product should not enter reference analysis prompt: %s", finalPrompt)
	}
	if strings.Contains(finalPrompt, "按当前参考素材直接生成出图方案") {
		t.Fatalf("reference fallback should not replace non-empty reference analysis: %s", finalPrompt)
	}
}

func TestSingleImageUnderstandingProjectsProviderPlaceholderRoleToPrimarySource(t *testing.T) {
	job := &models.EcommerceVisualDeconstructionJob{
		SourceReferenceID: "vsr_ref_primary",
		Metadata:          toJSONForTest(map[string]any{"image_understanding_policy": "single_image_per_runtime_job"}),
	}
	sourceIndex := map[string]string{"vsr_ref_primary": "reference"}
	elements := projectSingleImageUnderstandingElements(job, []InternalResultElementRequest{{
		SourceRole:        "sku|reference",
		SourceReferenceID: "SOURCE_REFERENCE_ID",
		ElementType:       "background",
		ElementKey:        "background_info",
		Label:             "图片中的背景信息",
		Value:             map[string]any{"description": "白色背景，柔和光线，极简商业摄影"},
		Readiness:         "ready",
	}}, sourceIndex)
	if len(elements) != 1 {
		t.Fatalf("placeholder provider role should be projected to primary source, got %#v", elements)
	}
	if elements[0].SourceRole != "reference" || elements[0].SourceReferenceID != "vsr_ref_primary" {
		t.Fatalf("expected reference projection, got role=%q source=%q", elements[0].SourceRole, elements[0].SourceReferenceID)
	}
}

func TestVisualDeconstructionPromptUsesConcreteSourceRoleAndID(t *testing.T) {
	prompt := visualDeconstructionUnderstandingPromptForSource("reference", "vsr_ref_primary")
	if strings.Contains(prompt, "sku|reference") || strings.Contains(prompt, "SOURCE_REFERENCE_ID") {
		t.Fatalf("prompt should not ask provider to echo placeholder role/id: %s", prompt)
	}
	for _, want := range []string{"source_role 必须填写 reference", "source_reference_id 必须填写 vsr_ref_primary"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing concrete source instruction %q: %s", want, prompt)
		}
	}
}

func TestCreatePromptPlannerJobUsesFallbackWhenManualTextMissing(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_low_quality_placeholder", "org_low_quality_placeholder", "SKU-LQP")
	session, err := service.CreateSession("user_low_quality_placeholder", "org_low_quality_placeholder", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, src := range []CreateSourceReferenceRequest{
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://sku", Metadata: map[string]any{"source_role": "sku"}},
		{SourceKind: models.VisualSourceKindUpload, SourceRef: "upload://reference", Metadata: map[string]any{"source_role": "reference"}},
	} {
		if _, err := service.CreateSourceReference("user_low_quality_placeholder", "org_low_quality_placeholder", session.ID, src); err != nil {
			t.Fatalf("create source reference: %v", err)
		}
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_lqp_sku", OrganizationID: "org_low_quality_placeholder", SessionID: session.ID, JobID: "vdj_lqp", ElementType: "product_fact", ElementKey: "provider_visual_description", Label: "weak sku", Confidence: 0.1, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"provider_text": "{}"}), Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "vde_lqp_ref", OrganizationID: "org_low_quality_placeholder", SessionID: session.ID, JobID: "vdj_lqp", ElementType: "reference_strategy", ElementKey: "style", Label: "weak ref", Confidence: 0.1, Readiness: "needs_review", ValueJSON: toJSONForTest(map[string]any{"style": ""}), Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed low quality element: %v", err)
		}
	}
	_, err = service.ApplyAttentionTree("org_low_quality_placeholder", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{
		{ElementID: "vde_lqp_sku", Decision: "keep", Question: "保留 SKU 原图产品？", Answer: "要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_product"}},
		{ElementID: "vde_lqp_sku", Decision: "drop", Question: "保留 SKU 原图背景？", Answer: "不要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "sku_background"}},
		{ElementID: "vde_lqp_ref", Decision: "drop", Question: "使用参考素材产品？", Answer: "不要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_product"}},
		{ElementID: "vde_lqp_ref", Decision: "keep", Question: "采用参考素材背景？", Answer: "要", Metadata: map[string]any{"fixed_prompt_question": true, "prompt_slot": "reference_background"}},
	}})
	if err != nil {
		t.Fatalf("apply fixed choices: %v", err)
	}
	service.WithPromptSnapshotCreator(&fakePromptSnapshotCreator{response: &promptcenter.PromptRunResponse{PromptID: "prompt_lq_default", ProductID: product.ID, SKUCode: product.SKUCode, TemplateID: "tpl_lq_default", SceneType: "product_composite", Status: "validated"}})
	resp, err := service.CreatePromptPlannerJob("org_low_quality_placeholder", session.ID, CreatePromptPlannerJobRequest{Marketplace: "amazon", Locale: "zh-CN", IdempotencyKey: "prompt-placeholder"})
	if err != nil {
		t.Fatalf("prompt planner with fallback image intent should not error: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("expected fallback prompt composition to complete, got %+v", resp)
	}
	model, err := service.repo.GetSession("org_low_quality_placeholder", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	plan := decodePromptPlan(model.PromptPlanJSON, model)
	finalPrompt, _ := plan.Variables["composed_prompt_text"].(string)
	for _, want := range []string{"按当前 SKU 图片直接生成", "按当前参考素材直接生成"} {
		if !strings.Contains(finalPrompt, want) {
			t.Fatalf("fallback prompt missing %q: %s", want, finalPrompt)
		}
	}
}

func TestCreateGenerationFanoutUsesPromptComposerFinalPrompt(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_composer", "org_fanout_composer", "SKU-FC")
	asset := models.EcommerceAsset{ID: "asset_fanout_composer", OrganizationID: "org_fanout_composer", UserID: "user_fanout_composer", AssetType: "image", SourceType: "upload", StorageKey: "store/composer.png", MimeType: "image/png", FileName: "composer.png", Width: 1024, Height: 1024, Metadata: "{}"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	session, err := service.CreateSession("user_fanout_composer", "org_fanout_composer", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_composer", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_composer", TemplateID: "tpl_base", Variables: map[string]any{"composed_prompt_text": "基础要求：保留 SKU 产品，采用参考素材背景。"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_fanout_composer", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	_, err = service.CreateGenerationFanout("org_fanout_composer", session.ID, CreateGenerationFanoutRequest{
		TemplateSlots:   []GenerationFanoutTemplateSlotRequest{{SourceAssetID: "asset_fanout_composer", TemplateID: "tpl_lifestyle", SceneTag: "生活方式图", DetailRequirement: "模板要求：浴室台面构图", NegativeRequirement: "不要文字水印"}},
		PromptVariables: map[string]any{"prompt_composer": map[string]any{"diy_prompt_text": "手动补充：高端酒店氛围", "negative_prompt_text": "不要畸形手指"}},
	})
	if err != nil {
		t.Fatalf("create fanout: %v", err)
	}
	if len(fake.createInputs) != 1 {
		t.Fatalf("expected one runtime job, got %d", len(fake.createInputs))
	}
	manifest := decodeObject(fake.createInputs[0].InputManifest)
	promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
	userPrompt := fmt.Sprint(promptSnapshot["user_prompt"])
	for _, want := range []string{"基础要求", "模板要求：浴室台面构图", "手动补充：高端酒店氛围"} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("runtime user_prompt missing %q: %s", want, userPrompt)
		}
	}
	stylePrompt := fmt.Sprint(promptSnapshot["style_prompt"])
	if !strings.Contains(stylePrompt, "不要文字水印") || !strings.Contains(stylePrompt, "不要畸形手指") {
		t.Fatalf("runtime negative prompt missing slot/manual negatives: %s", stylePrompt)
	}
}

func TestCreateGenerationFanoutIncludesReferenceAssetsAndTemplateSpecificRuntimeParams(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_diversity", "org_fanout_diversity", "SKU-FD")
	for _, asset := range []models.EcommerceAsset{
		{ID: "asset_diversity_sku", OrganizationID: "org_fanout_diversity", UserID: "user_fanout_diversity", AssetType: "image", SourceType: "upload", StorageKey: "store/sku.png", MimeType: "image/png", FileName: "sku.png", Width: 1200, Height: 1200, Metadata: toJSONForTest(map[string]any{"source_role": "sku"})},
		{ID: "asset_diversity_ref", OrganizationID: "org_fanout_diversity", UserID: "user_fanout_diversity", AssetType: "image", SourceType: "upload", StorageKey: "store/ref.png", MimeType: "image/png", FileName: "ref.png", Width: 1200, Height: 1200, Metadata: toJSONForTest(map[string]any{"source_role": "reference"})},
	} {
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout_diversity", "org_fanout_diversity", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_diversity", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_diversity", TemplateID: "tpl_base", Variables: map[string]any{"composed_prompt_text": "基础要求：白色头戴式耳机，参考深色桌面氛围与侧逆光。"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_diversity_sku", Role: "sku"}, {AssetID: "asset_diversity_ref", Role: "reference"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	_, err = service.CreateGenerationFanout("org_fanout_diversity", session.ID, CreateGenerationFanoutRequest{
		IdempotencyKey: "fanout-diversity",
		TemplateSlots: []GenerationFanoutTemplateSlotRequest{
			{SourceAssetID: "asset_diversity_sku", TemplateID: "amazon-hero", SceneTag: "主图"},
			{SourceAssetID: "asset_diversity_sku", TemplateID: "industrial-poster", SceneTag: "海报"},
			{SourceAssetID: "asset_diversity_sku", TemplateID: "lifestyle-scene", SceneTag: "使用图"},
		},
		RequestedVariants: 1,
		ProviderConfig:    map[string]any{"resolution_id": "1024-square"},
	})
	if err != nil {
		t.Fatalf("create fanout diversity: %v", err)
	}
	wantDims := map[string]string{"amazon-hero": "1024x1024", "industrial-poster": "768x1024", "lifestyle-scene": "1365x768"}
	for _, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		templateID := fmt.Sprint(params["template_id"])
		gotDims := fmt.Sprintf("%vx%v", params["width"], params["height"])
		if gotDims != wantDims[templateID] {
			t.Fatalf("template %s should use distinct runtime dimensions %s, got %s params=%#v", templateID, wantDims[templateID], gotDims, params)
		}
		sourceIDs, _ := manifest["source_asset_ids"].([]any)
		if len(sourceIDs) != 2 || fmt.Sprint(sourceIDs[0]) != "asset_diversity_sku" || fmt.Sprint(sourceIDs[1]) != "asset_diversity_ref" {
			t.Fatalf("fanout runtime should include selected SKU plus reference asset, got %#v", sourceIDs)
		}
		promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
		userPrompt := fmt.Sprint(promptSnapshot["user_prompt"])
		for _, want := range []string{"强制差异化", "不得与其他槽位构图相同", templateID} {
			if !strings.Contains(userPrompt, want) {
				t.Fatalf("fanout prompt missing diversity marker %q for template %s: %s", want, templateID, userPrompt)
			}
		}
	}
}

func TestCreateGenerationFanoutHonorsExplicitTemplateSlots(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_fanout_slots", "org_fanout_slots", "SKU-FS")
	for _, asset := range []models.EcommerceAsset{
		{ID: "asset_slot_1", OrganizationID: "org_fanout_slots", UserID: "user_fanout_slots", AssetType: "image", SourceType: "upload", StorageKey: "store/slot-a.png", MimeType: "image/png", FileName: "slot-a.png", Width: 1024, Height: 1024, Metadata: "{}"},
		{ID: "asset_slot_2", OrganizationID: "org_fanout_slots", UserID: "user_fanout_slots", AssetType: "image", SourceType: "upload", StorageKey: "store/slot-b.png", MimeType: "image/png", FileName: "slot-b.png", Width: 1024, Height: 1024, Metadata: "{}"},
	} {
		if err := db.Create(&asset).Error; err != nil {
			t.Fatalf("seed asset: %v", err)
		}
	}
	session, err := service.CreateSession("user_fanout_slots", "org_fanout_slots", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	model, err := service.repo.GetSession("org_fanout_slots", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	model.PromptPlanJSON = encodePromptPlan(&PromptPlanDTO{SchemaVersion: promptPlanSchemaVersion, Status: "ready", PromptID: "prompt_slots", TemplateID: "tpl_base", Variables: map[string]any{"prompt": "Generate ecommerce hero"}, SourceAssets: []PromptPlanSourceAssetDTO{{AssetID: "asset_slot_1", Role: "sku"}}})
	if err := service.repo.SaveSession(model); err != nil {
		t.Fatalf("save prompt plan: %v", err)
	}
	fake := &fakeRuntimeCapabilityReader{matrix: readyVisualGenerationMatrix()}
	service.WithRuntimeOrchestrator(fake)
	resp, err := service.CreateGenerationFanout("org_fanout_slots", session.ID, CreateGenerationFanoutRequest{
		IdempotencyKey: "fanout-slots",
		SourceAssetIDs: []string{"asset_slot_1", "asset_slot_2"},
		TemplateIDs:    []string{"amazon-hero", "industrial-poster", "lifestyle-scene"},
		TemplateSlots: []GenerationFanoutTemplateSlotRequest{
			{SourceAssetID: "asset_slot_1", TemplateID: "amazon-hero", SceneTag: "主图", DetailRequirement: "主体完整且白底", NegativeRequirement: "不要文字水印"},
			{SourceAssetID: "asset_slot_2", TemplateID: "industrial-poster", SceneTag: "细节图", DetailRequirement: "突出金属纹理"},
			{SourceAssetID: "asset_slot_1", TemplateID: "lifestyle-scene", SceneTag: "使用图", DetailRequirement: "展示真实使用场景"},
		},
		RequestedVariants: 1,
		ProviderConfig: map[string]any{
			"dimensions":      "1280×720",
			"negative_prompt": "避免杂乱背景和主体丢失",
		},
	})
	if err != nil {
		t.Fatalf("create explicit fanout: %v", err)
	}
	if len(resp.Items) != 3 || len(fake.createInputs) != 3 {
		t.Fatalf("expected exactly 3 explicit fanout jobs, resp=%+v inputs=%d", resp, len(fake.createInputs))
	}
	for idx, input := range fake.createInputs {
		manifest := decodeObject(input.InputManifest)
		params, _ := manifest["params_snapshot"].(map[string]any)
		if fmt.Sprint(params["fanout_total"]) != "3" || fmt.Sprint(params["fanout_index"]) != fmt.Sprint(idx) {
			t.Fatalf("explicit fanout index/total mismatch: %#v", params)
		}
		if fmt.Sprint(params["scene_tag"]) == "" || fmt.Sprint(params["detail_requirement"]) == "" {
			t.Fatalf("explicit fanout slot requirements missing from runtime params: %#v", params)
		}
		if fmt.Sprint(params["width"]) != "1280" || fmt.Sprint(params["height"]) != "720" || fmt.Sprint(params["aspect_ratio"]) != "16:9" || fmt.Sprint(params["resolution_id"]) != "1280x720" {
			t.Fatalf("frontend dimensions should reach runtime params without stale template resolution_id, got %#v", params)
		}
		if fmt.Sprint(params["negative_prompt"]) != "避免杂乱背景和主体丢失" {
			t.Fatalf("frontend negative prompt should reach runtime params, got %#v", params)
		}
		promptSnapshot, _ := manifest["prompt_snapshot"].(map[string]any)
		if !strings.Contains(fmt.Sprint(promptSnapshot["user_prompt"]), fmt.Sprint(params["detail_requirement"])) {
			t.Fatalf("slot detail requirement not appended to provider prompt: prompt=%#v params=%#v", promptSnapshot, params)
		}
		if idx == 0 && !strings.Contains(fmt.Sprint(promptSnapshot["style_prompt"]), "不要文字水印") {
			t.Fatalf("slot negative requirement not appended to negative prompt: %#v", promptSnapshot)
		}
	}
}

func TestApplyAttentionTreePersistsLayeredDecisionNodes(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	product := seedProduct(t, db, "prod_tree", "org_tree", "SKU-T")
	session, err := service.CreateSession("user_tree", "org_tree", CreateSessionRequest{ProductID: product.ID, SKUCode: product.SKUCode})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, element := range []models.EcommerceVisualDeconstructionElement{
		{ID: "vde_root", OrganizationID: "org_tree", SessionID: session.ID, JobID: "vdj_tree", ElementType: "style", ElementKey: "root", Label: "root"},
		{ID: "vde_child", OrganizationID: "org_tree", SessionID: session.ID, JobID: "vdj_tree", ElementType: "object", ElementKey: "child", Label: "child"},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("seed element: %v", err)
		}
	}
	layer0 := 0
	layer1 := 1
	confidence := 0.9
	_, err = service.ApplyAttentionTree("org_tree", session.ID, ApplyAttentionTreeRequest{TreeID: "tree-1", RoundID: "round-1", Decisions: []AttentionDecisionInput{
		{ElementID: "vde_root", Decision: "keep", DecisionNodeID: "node-root", Layer: &layer0, Question: "Keep root?", Answer: "yes", Confidence: &confidence},
		{ElementID: "vde_child", Decision: "replace", DecisionNodeID: "node-child", ParentNodeID: "node-root", Layer: &layer1, Path: []string{"root", "child"}, Question: "Replace child?", Answer: "replace"},
	}})
	if err != nil {
		t.Fatalf("apply layered attention tree: %v", err)
	}
	model, err := service.repo.GetSession("org_tree", session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	intent := decodeIntentSpec(model.IntentSpecJSON, model)
	manifest, _ := intent.Metadata["input_manifest"].(map[string]any)
	tree, _ := manifest["decision_tree"].(map[string]any)
	nodes, _ := tree["nodes"].([]any)
	if tree["schema_version"] != "visual-decision-tree.v1" || len(nodes) != 2 {
		t.Fatalf("decision tree projection missing: %#v", tree)
	}
	if _, err := service.ApplyAttentionTree("org_tree", session.ID, ApplyAttentionTreeRequest{Decisions: []AttentionDecisionInput{{ElementID: "vde_root", Decision: "keep", DecisionNodeID: "bad-root", ParentNodeID: "parent", Layer: &layer0}}}); err == nil {
		t.Fatalf("expected invalid root parent error")
	}
}

func readyVisualGenerationMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "image_generation", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyIntentPlanningMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "intent_planning", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyPromptPlanningMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "prompt_planning", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func readyStrategyReportMatrix() *platform.RuntimeCapabilityMatrix {
	return &platform.RuntimeCapabilityMatrix{ProductCode: "ecommerce", Items: []platform.RuntimeCapabilityItem{{TaskType: "strategy_report", Status: "ready", Available: true, ContractStatus: "ready"}}}
}

func toJSONForTest(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSaveGenerationVersionAsTemplateFromCompletedResult(t *testing.T) {
	service, _, db := setupVisualWorkflowTest(t)
	if err := db.AutoMigrate(&models.SavedTemplate{}); err != nil {
		t.Fatalf("migrate saved template: %v", err)
	}
	workspaceRepo := repository.NewWorkspaceRepository(db)
	service.WithWorkspaceRepository(workspaceRepo)
	product, session, version := seedWritebackVisualWorkflow(t, service, db, "asset_tpl_1")
	completedStatus := "completed"
	completedStage := "result_available"
	selectedAssetID := "asset_tpl_1"
	version, err := service.UpdateGenerationVersion("org_1", session.ID, version.VersionID, UpdateGenerationVersionRequest{Status: &completedStatus, Stage: &completedStage, ResultAssets: []ResultAssetDTO{{AssetID: selectedAssetID, AssetContentURL: "/api/v1/ecommerce/assets/" + selectedAssetID + "/content"}}, SelectedResultAssetID: &selectedAssetID})
	if err != nil {
		t.Fatalf("prepare completed version: %v", err)
	}
	resp, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{Title: "Hero template", IdempotencyKey: "idem-template"})
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	if resp.ProductID != product.ID || resp.SelectedResultAssetID != "asset_tpl_1" || resp.Template.SourceType != "visual_generation_result" {
		t.Fatalf("unexpected template response: %+v", resp)
	}
	if resp.Template.ID == "" || resp.Template.ZH.Title != "Hero template" || resp.AssetContentURL == "" {
		t.Fatalf("missing template fields: %+v", resp)
	}
	items, err := workspaceRepo.ListSavedTemplates(repository.Scope{UserID: "user_1", OrgID: "org_1"})
	if err != nil || len(items) != 1 {
		t.Fatalf("saved templates not persisted: len=%d err=%v", len(items), err)
	}
	replay, err := service.SaveGenerationVersionAsTemplate("user_1", "org_1", session.ID, version.VersionID, SaveGenerationTemplateRequest{Title: "Hero template", IdempotencyKey: "idem-template"})
	if err != nil {
		t.Fatalf("replay save template: %v", err)
	}
	if replay.Template.ID != resp.Template.ID || len(replay.SavedTemplates) != 1 {
		t.Fatalf("expected idempotent template save, got first=%s replay=%s len=%d", resp.Template.ID, replay.Template.ID, len(replay.SavedTemplates))
	}
}
