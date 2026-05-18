package visualworkflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
)

type CreateSessionRequest struct {
	ProductID         string `json:"product_id"`
	SKUCode           string `json:"sku_code" binding:"required"`
	ToolSlug          string `json:"tool_slug"`
	TemplateID        string `json:"template_id"`
	TemplateVersionID string `json:"template_version_id"`
	IdempotencyKey    string `json:"idempotency_key"`
}

type UpdateSessionRequest struct {
	CurrentStage       string                 `json:"current_stage"`
	Status             string                 `json:"status"`
	TemplateID         string                 `json:"template_id"`
	TemplateVersionID  string                 `json:"template_version_id"`
	Readiness          map[string]any         `json:"readiness"`
	IntentSpec         *IntentSpecDTO         `json:"intent_spec"`
	PromptPlan         *PromptPlanDTO         `json:"prompt_plan"`
	GenerationVersions []GenerationVersionDTO `json:"generation_versions"`
	Metadata           map[string]any         `json:"metadata"`
}

type CreateGenerationVersionRequest struct {
	IdempotencyKey        string             `json:"idempotency_key"`
	ParentVersionID       string             `json:"parent_version_id"`
	SourceVersionID       string             `json:"source_version_id"`
	PromptID              string             `json:"prompt_id"`
	RefinementInstruction string             `json:"refinement_instruction"`
	MaskAssetID           string             `json:"mask_asset_id"`
	Status                string             `json:"status"`
	Stage                 string             `json:"stage"`
	Progress              *int               `json:"progress"`
	RuntimeJobID          string             `json:"runtime_job_id"`
	ImageJobID            string             `json:"image_job_id"`
	ResultAssets          []ResultAssetDTO   `json:"result_assets"`
	SelectedResultAssetID string             `json:"selected_result_asset_id"`
	Blockers              []ReadinessBlocker `json:"blockers"`
	Metadata              map[string]any     `json:"metadata"`
}

func (r *CreateGenerationVersionRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := validateEmptyClientJobReferences(raw); err != nil {
		return err
	}
	if err := validateNoExecutionArtifacts(raw); err != nil {
		return err
	}
	if err := validateNoClientClosureEvidenceArtifacts(raw); err != nil {
		return err
	}
	type alias CreateGenerationVersionRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = CreateGenerationVersionRequest(out)
	return nil
}

type UpdateGenerationVersionRequest struct {
	Status                *string            `json:"status"`
	Stage                 *string            `json:"stage"`
	Progress              *int               `json:"progress"`
	RuntimeJobID          *string            `json:"runtime_job_id"`
	ImageJobID            *string            `json:"image_job_id"`
	ResultAssets          []ResultAssetDTO   `json:"result_assets"`
	SelectedResultAssetID *string            `json:"selected_result_asset_id"`
	RefinementInstruction *string            `json:"refinement_instruction"`
	MaskAssetID           *string            `json:"mask_asset_id"`
	SourceVersionID       *string            `json:"source_version_id"`
	ParentVersionID       *string            `json:"parent_version_id"`
	Blockers              []ReadinessBlocker `json:"blockers"`
	Metadata              map[string]any     `json:"metadata"`
}

func (r *UpdateGenerationVersionRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["version_id"]; ok {
		return fmt.Errorf("version_id cannot be updated")
	}
	if _, ok := raw["session_id"]; ok {
		return fmt.Errorf("session_id cannot be updated")
	}
	if err := validateEmptyClientJobReferences(raw); err != nil {
		return err
	}
	if err := validateNoExecutionArtifacts(raw); err != nil {
		return err
	}
	if err := validateNoClientClosureEvidenceArtifacts(raw); err != nil {
		return err
	}
	type alias UpdateGenerationVersionRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = UpdateGenerationVersionRequest(out)
	return nil
}

type SelectGenerationVersionRequest struct {
	SelectedResultAssetID string         `json:"selected_result_asset_id"`
	Metadata              map[string]any `json:"metadata"`
}

type SaveGenerationTemplateRequest struct {
	AssetID        string   `json:"asset_id,omitempty"`
	Title          string   `json:"title,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Scenario       string   `json:"scenario,omitempty"`
	Platform       string   `json:"platform,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

type SaveGenerationTemplateResponse struct {
	SessionID             string                           `json:"session_id"`
	VersionID             string                           `json:"version_id"`
	ProductID             string                           `json:"product_id"`
	SKUCode               string                           `json:"sku_code"`
	SelectedResultAssetID string                           `json:"selected_result_asset_id"`
	AssetContentURL       string                           `json:"asset_content_url,omitempty"`
	Template              repository.SavedTemplateRecord   `json:"template"`
	SavedTemplates        []repository.SavedTemplateRecord `json:"saved_templates"`
	GenerationVersion     GenerationVersionDTO             `json:"generation_version"`
}

type WritebackSelectedGenerationAssetRequest struct {
	AssetID        string         `json:"asset_id,omitempty"`
	AssetRole      string         `json:"asset_role,omitempty"`
	IsPrimary      *bool          `json:"is_primary,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (r *WritebackSelectedGenerationAssetRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := validateNoWritebackMetadataArtifacts(raw); err != nil {
		return err
	}
	type alias WritebackSelectedGenerationAssetRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if err := validateNoWritebackMetadataArtifacts(out.Metadata); err != nil {
		return err
	}
	*r = WritebackSelectedGenerationAssetRequest(out)
	return nil
}

type AssetRelationDTO struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	AssetID        string         `json:"asset_id"`
	OwnerType      string         `json:"owner_type"`
	OwnerID        string         `json:"owner_id"`
	RelationType   string         `json:"relation_type"`
	AssetRole      string         `json:"asset_role"`
	IsPrimary      bool           `json:"is_primary"`
	PlatformCode   string         `json:"platform_code,omitempty"`
	SiteCode       string         `json:"site_code,omitempty"`
	LocaleCode     string         `json:"locale_code,omitempty"`
	SortOrder      int            `json:"sort_order"`
	Visibility     string         `json:"visibility"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type WritebackSelectedGenerationAssetResponse struct {
	SessionID             string               `json:"session_id"`
	VersionID             string               `json:"version_id"`
	ProductID             string               `json:"product_id"`
	SKUCode               string               `json:"sku_code"`
	SelectedResultAssetID string               `json:"selected_result_asset_id"`
	AssetRelation         AssetRelationDTO     `json:"asset_relation"`
	Idempotent            bool                 `json:"idempotent"`
	GenerationVersion     GenerationVersionDTO `json:"generation_version"`
}

func (r *SelectGenerationVersionRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := validateNoExecutionArtifacts(raw); err != nil {
		return err
	}
	if err := validateNoClientClosureEvidenceArtifacts(raw); err != nil {
		return err
	}
	type alias SelectGenerationVersionRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = SelectGenerationVersionRequest(out)
	return nil
}

func validateEmptyClientJobReferences(raw map[string]any) error {
	for _, field := range []string{"runtime_job_id", "image_job_id"} {
		value, ok := raw[field]
		if !ok || value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) != "" {
			return fmt.Errorf("%s is not client-writeable in this tranche", field)
		}
	}
	return nil
}

type CreateSourceReferenceRequest struct {
	SourceKind      string         `json:"source_kind" binding:"required"`
	SourceRef       string         `json:"source_ref"`
	SourceURL       string         `json:"source_url"`
	AssetID         string         `json:"asset_id"`
	AssetRelationID string         `json:"asset_relation_id"`
	MimeType        string         `json:"mime_type"`
	Metadata        map[string]any `json:"metadata"`
}

type UpdateSourceReferenceRequest struct {
	Status        string         `json:"status"`
	ResolveStatus string         `json:"resolve_status"`
	ErrorCode     string         `json:"error_code"`
	ErrorMessage  string         `json:"error_message"`
	Metadata      map[string]any `json:"metadata"`
}

type CreateDeconstructionJobRequest struct {
	// SourceReferenceID is kept for compatibility with older single-source callers.
	// V2 P0 deconstruction now resolves both SKU and reference tracks from the
	// session and records them in the runtime input manifest.
	SourceReferenceID string         `json:"source_reference_id"`
	IdempotencyKey    string         `json:"idempotency_key"`
	RequestedElements []string       `json:"requested_elements"`
	Metadata          map[string]any `json:"metadata"`
}

type InternalRuntimeUpdateRequest struct {
	Status       string `json:"status"`
	Stage        string `json:"stage"`
	StageMessage string `json:"stage_message"`
	Progress     *int   `json:"progress"`
	RuntimeJobID string `json:"runtime_job_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type InternalResultElementRequest struct {
	ElementType       string         `json:"element_type"`
	ElementKey        string         `json:"element_key"`
	Key               string         `json:"key"`
	Label             string         `json:"label"`
	Value             map[string]any `json:"value"`
	BoundingBox       map[string]any `json:"bounding_box"`
	Confidence        float64        `json:"confidence"`
	Selected          bool           `json:"selected"`
	Confirmed         bool           `json:"confirmed"`
	Readiness         string         `json:"readiness"`
	SortOrder         int            `json:"sort_order"`
	MaskAssetID       string         `json:"mask_asset_id"`
	SourceAssetID     string         `json:"source_asset_id"`
	SourceReferenceID string         `json:"source_reference_id"`
	SourceRole        string         `json:"source_role"`
	Metadata          map[string]any `json:"metadata"`
}

type InternalResultMetadataRequest struct {
	DeconstructionElements []InternalResultElementRequest `json:"deconstruction_elements"`
}

type InternalRecordResultsRequest struct {
	Status                 string                         `json:"status"`
	Progress               int                            `json:"progress"`
	Stage                  string                         `json:"stage"`
	StageMessage           string                         `json:"stage_message,omitempty"`
	ErrorCode              string                         `json:"error_code,omitempty"`
	ErrorMessage           string                         `json:"error_message,omitempty"`
	Elements               []InternalResultElementRequest `json:"elements"`
	DeconstructionElements []InternalResultElementRequest `json:"deconstruction_elements"`
	Metadata               InternalResultMetadataRequest  `json:"metadata,omitempty"`
	Variants               []map[string]any               `json:"variants,omitempty"`
}

type UpdateElementRequest struct {
	Selected      *bool          `json:"selected"`
	Decision      string         `json:"decision"`
	GroupPath     []string       `json:"group_path"`
	TargetAssetID string         `json:"target_asset_id"`
	Rationale     string         `json:"rationale"`
	Confidence    *float64       `json:"confidence"`
	Confirmed     *bool          `json:"confirmed"`
	Readiness     string         `json:"readiness"`
	Label         string         `json:"label"`
	Value         map[string]any `json:"value"`
	Metadata      map[string]any `json:"metadata"`
	BoundingBox   map[string]any `json:"bounding_box"`
}

type AttentionDecisionInput struct {
	ElementID      string         `json:"element_id" binding:"required"`
	Decision       string         `json:"decision" binding:"required"`
	GroupPath      []string       `json:"group_path"`
	TargetAssetID  string         `json:"target_asset_id"`
	Rationale      string         `json:"rationale"`
	Confidence     *float64       `json:"confidence"`
	DecisionNodeID string         `json:"decision_node_id"`
	ParentNodeID   string         `json:"parent_node_id"`
	RoundID        string         `json:"round_id"`
	Layer          *int           `json:"layer"`
	Path           []string       `json:"path"`
	Question       string         `json:"question"`
	Answer         string         `json:"answer"`
	Metadata       map[string]any `json:"metadata"`
}

type ApplyAttentionTreeRequest struct {
	TreeID        string                   `json:"tree_id"`
	RoundID       string                   `json:"round_id"`
	Layer         *int                     `json:"layer"`
	Decisions     []AttentionDecisionInput `json:"decisions" binding:"required"`
	DriftControls map[string]any           `json:"drift_controls"`
}

const MaxGenerationFanoutTasks = 20

type GenerationFanoutTemplateSlotRequest struct {
	SourceAssetID     string `json:"source_asset_id"`
	TemplateID        string `json:"template_id"`
	TemplateVersionID string `json:"template_version_id"`
}

type CreateGenerationFanoutRequest struct {
	IdempotencyKey     string                                `json:"idempotency_key"`
	SourceAssetIDs     []string                              `json:"source_asset_ids"`
	TemplateIDs        []string                              `json:"template_ids"`
	TemplateVersionIDs []string                              `json:"template_version_ids"`
	TemplateSlots      []GenerationFanoutTemplateSlotRequest `json:"template_slots"`
	Marketplace        string                                `json:"marketplace"`
	Locale             string                                `json:"locale"`
	SceneType          string                                `json:"scene_type"`
	RequestedVariants  int                                   `json:"requested_variants"`
	ProviderConfig     map[string]any                        `json:"provider_config"`
	PromptVariables    map[string]any                        `json:"prompt_variables"`
	Metadata           map[string]any                        `json:"metadata"`
}

type GenerationFanoutItemDTO struct {
	FanoutTaskID      string               `json:"fanout_task_id"`
	SourceAssetID     string               `json:"source_asset_id"`
	TemplateID        string               `json:"template_id"`
	TemplateVersionID string               `json:"template_version_id,omitempty"`
	SlotIndex         int                  `json:"slot_index"`
	GenerationVersion GenerationVersionDTO `json:"generation_version"`
}

type CreateGenerationFanoutResponse struct {
	SessionID string                    `json:"session_id"`
	ProductID string                    `json:"product_id"`
	SKUCode   string                    `json:"sku_code"`
	FanoutID  string                    `json:"fanout_id"`
	Items     []GenerationFanoutItemDTO `json:"items"`
	Blockers  []ReadinessBlocker        `json:"blockers,omitempty"`
}

type ConfirmSelectionRequest struct {
	ElementIDs []string `json:"element_ids" binding:"required"`
}

type CreateIntentPlannerJobRequest struct {
	SourceReferenceID string         `json:"source_reference_id"`
	ElementIDs        []string       `json:"element_ids"`
	Marketplace       string         `json:"marketplace"`
	Locale            string         `json:"locale"`
	DriftControls     map[string]any `json:"drift_controls"`
	IdempotencyKey    string         `json:"idempotency_key"`
	Metadata          map[string]any `json:"metadata"`
}

type IntentPlannerJobResponse struct {
	SessionID      string             `json:"session_id"`
	RuntimeJobID   string             `json:"runtime_job_id,omitempty"`
	Status         string             `json:"status"`
	Stage          string             `json:"stage"`
	Progress       int                `json:"progress"`
	Blockers       []ReadinessBlocker `json:"blockers,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
}

type CreatePromptPlannerJobRequest struct {
	PromptID        string         `json:"prompt_id"`
	TemplateID      string         `json:"template_id"`
	Marketplace     string         `json:"marketplace"`
	Locale          string         `json:"locale"`
	DriftControls   map[string]any `json:"drift_controls"`
	PromptVariables map[string]any `json:"prompt_variables"`
	IdempotencyKey  string         `json:"idempotency_key"`
}

type PromptPlannerJobResponse = IntentPlannerJobResponse

type CreateStrategyReportJobRequest struct {
	Marketplace    string         `json:"marketplace"`
	Locale         string         `json:"locale"`
	ReportGoal     string         `json:"report_goal"`
	SourceFacts    map[string]any `json:"source_facts"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type StrategyReportJobResponse = IntentPlannerJobResponse

type ReadinessBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

type ReadinessDTO struct {
	Overall        string             `json:"overall"`
	Source         string             `json:"source"`
	Deconstruction string             `json:"deconstruction"`
	Prompt         string             `json:"prompt"`
	Generation     string             `json:"generation"`
	Blockers       []ReadinessBlocker `json:"blockers,omitempty"`
}

type IntentSpecDTO struct {
	SchemaVersion string             `json:"schema_version"`
	SceneType     string             `json:"scene_type,omitempty"`
	ToolSlug      string             `json:"tool_slug,omitempty"`
	ProductID     string             `json:"product_id,omitempty"`
	SKUCode       string             `json:"sku_code,omitempty"`
	Source        IntentSourceDTO    `json:"source,omitempty"`
	Selections    []IntentElementDTO `json:"selections,omitempty"`
	Requirements  map[string]any     `json:"requirements,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

type IntentSourceDTO struct {
	SourceKind        string                     `json:"source_kind,omitempty"`
	SourceReferenceID string                     `json:"source_reference_id,omitempty"`
	AssetID           string                     `json:"asset_id,omitempty"`
	AssetRelationID   string                     `json:"asset_relation_id,omitempty"`
	SourceRef         string                     `json:"source_ref,omitempty"`
	SourceReferences  []IntentSourceReferenceDTO `json:"source_references,omitempty"`
	Metadata          map[string]any             `json:"metadata,omitempty"`
}

type IntentSourceReferenceDTO struct {
	SourceReferenceID string         `json:"source_reference_id"`
	Role              string         `json:"role"`
	SourceKind        string         `json:"source_kind,omitempty"`
	AssetID           string         `json:"asset_id,omitempty"`
	AssetRelationID   string         `json:"asset_relation_id,omitempty"`
	SourceRef         string         `json:"source_ref,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type IntentElementDTO struct {
	ElementID     string         `json:"element_id,omitempty"`
	ElementType   string         `json:"element_type,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	GroupPath     []string       `json:"group_path,omitempty"`
	TargetAssetID string         `json:"target_asset_id,omitempty"`
	ElementKey    string         `json:"element_key,omitempty"`
	Label         string         `json:"label,omitempty"`
	Value         map[string]any `json:"value,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type PromptPlanDTO struct {
	SchemaVersion     string                     `json:"schema_version"`
	Status            string                     `json:"status"`
	PromptID          string                     `json:"prompt_id,omitempty"`
	SceneType         string                     `json:"scene_type,omitempty"`
	TemplateID        string                     `json:"template_id,omitempty"`
	TemplateVersionID string                     `json:"template_version_id,omitempty"`
	Variables         map[string]any             `json:"variables,omitempty"`
	SourceAssets      []PromptPlanSourceAssetDTO `json:"source_assets,omitempty"`
	Blockers          []ReadinessBlocker         `json:"blockers,omitempty"`
	Metadata          map[string]any             `json:"metadata,omitempty"`
}

type PromptPlanSourceAssetDTO struct {
	AssetID           string         `json:"asset_id,omitempty"`
	AssetRelationID   string         `json:"asset_relation_id,omitempty"`
	SourceReferenceID string         `json:"source_reference_id,omitempty"`
	Role              string         `json:"role,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type SessionDTO struct {
	ID                 string                 `json:"id"`
	OrganizationID     string                 `json:"organization_id"`
	UserID             string                 `json:"user_id,omitempty"`
	ProductID          string                 `json:"product_id"`
	SKUCode            string                 `json:"sku_code"`
	ToolSlug           string                 `json:"tool_slug,omitempty"`
	TemplateID         string                 `json:"template_id,omitempty"`
	TemplateVersionID  string                 `json:"template_version_id,omitempty"`
	CurrentStage       string                 `json:"current_stage"`
	Status             string                 `json:"status"`
	Readiness          ReadinessDTO           `json:"readiness"`
	IntentSpec         IntentSpecDTO          `json:"intent_spec"`
	PromptPlan         PromptPlanDTO          `json:"prompt_plan"`
	GenerationVersions []GenerationVersionDTO `json:"generation_versions"`
	IdempotencyKey     string                 `json:"idempotency_key,omitempty"`
	Metadata           map[string]any         `json:"metadata,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type promptPlanAlias PromptPlanDTO

func (p *PromptPlanDTO) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := validatePromptPlanForbiddenKeys(raw); err != nil {
		return err
	}
	var alias promptPlanAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*p = PromptPlanDTO(alias)
	return nil
}

func validatePromptPlanForbiddenKeys(raw any) error {
	if key, ok := findForbiddenExecutionArtifactKey(raw); ok {
		return fmt.Errorf("prompt_plan contains Prompt Center-owned field %q", key)
	}
	return nil
}

func validateNoExecutionArtifacts(raw any) error {
	if key, ok := findForbiddenExecutionArtifactKey(raw); ok {
		return fmt.Errorf("generation version payload contains execution-owned field %q", key)
	}
	return nil
}

func validateNoClientClosureEvidenceArtifacts(raw any) error {
	if key, ok := findForbiddenClientClosureEvidenceKey(raw); ok {
		return fmt.Errorf("generation version payload contains server-owned closure evidence field %q", key)
	}
	return nil
}

func validateNoWritebackMetadataArtifacts(raw any) error {
	if key, ok := findForbiddenWritebackMetadataKey(raw); ok {
		return fmt.Errorf("writeback metadata contains execution-owned field %q", key)
	}
	return nil
}

type SourceReferenceDTO struct {
	ID                string         `json:"id"`
	SourceKind        string         `json:"source_kind"`
	SourceRef         string         `json:"source_ref,omitempty"`
	AssetID           string         `json:"asset_id,omitempty"`
	AssetRelationID   string         `json:"asset_relation_id,omitempty"`
	AssetContentURL   string         `json:"asset_content_url,omitempty"`
	MimeType          string         `json:"mime_type,omitempty"`
	Status            string         `json:"status"`
	ResolveStatus     string         `json:"resolve_status"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
	ErrorCode         string         `json:"error_code,omitempty"`
	ErrorMessage      string         `json:"error_message,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type DeconstructionJobDTO struct {
	JobID             string `json:"job_id"`
	RuntimeJobID      string `json:"runtime_job_id,omitempty"`
	Status            string `json:"status"`
	Stage             string `json:"stage,omitempty"`
	StageMessage      string `json:"stage_message,omitempty"`
	Progress          int    `json:"progress"`
	CapabilityCode    string `json:"capability_code,omitempty"`
	RuntimeTaskType   string `json:"runtime_task_type,omitempty"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
}

type DeconstructionElementDTO struct {
	ID                  string         `json:"id"`
	ElementType         string         `json:"element_type"`
	ElementKey          string         `json:"element_key,omitempty"`
	Label               string         `json:"label,omitempty"`
	Confidence          float64        `json:"confidence"`
	BoundingBox         map[string]any `json:"bounding_box,omitempty"`
	MaskAssetID         string         `json:"mask_asset_id,omitempty"`
	MaskAssetContentURL string         `json:"mask_asset_content_url,omitempty"`
	SourceAssetID       string         `json:"source_asset_id,omitempty"`
	SourceReferenceID   string         `json:"source_reference_id,omitempty"`
	SourceRole          string         `json:"source_role,omitempty"`
	Decision            string         `json:"decision,omitempty"`
	Value               map[string]any `json:"value"`
	Readiness           string         `json:"readiness"`
	Selected            bool           `json:"selected"`
	Confirmed           bool           `json:"confirmed"`
	SortOrder           int            `json:"sort_order"`
}

type GenerationVersionDTO struct {
	VersionID             string             `json:"version_id"`
	ParentVersionID       string             `json:"parent_version_id,omitempty"`
	SourceVersionID       string             `json:"source_version_id,omitempty"`
	PromptID              string             `json:"prompt_id,omitempty"`
	PromptPlanStatus      string             `json:"prompt_plan_status,omitempty"`
	IntentSpecSnapshot    map[string]any     `json:"intent_spec_snapshot,omitempty"`
	RefinementInstruction string             `json:"refinement_instruction,omitempty"`
	MaskAssetID           string             `json:"mask_asset_id,omitempty"`
	ImageJobID            string             `json:"image_job_id,omitempty"`
	RuntimeJobID          string             `json:"runtime_job_id,omitempty"`
	SelectedResultAssetID string             `json:"selected_result_asset_id,omitempty"`
	Status                string             `json:"status"`
	Stage                 string             `json:"stage,omitempty"`
	Progress              int                `json:"progress,omitempty"`
	ResultAssets          []ResultAssetDTO   `json:"result_assets,omitempty"`
	Blockers              []ReadinessBlocker `json:"blockers,omitempty"`
	CreatedAt             string             `json:"created_at,omitempty"`
	UpdatedAt             string             `json:"updated_at,omitempty"`
	Metadata              map[string]any     `json:"metadata,omitempty"`
}

func (v *GenerationVersionDTO) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := validateNoExecutionArtifacts(raw); err != nil {
		return err
	}
	type alias GenerationVersionDTO
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*v = GenerationVersionDTO(out)
	return nil
}

type ResultAssetDTO struct {
	AssetID         string         `json:"asset_id"`
	AssetContentURL string         `json:"asset_content_url"`
	Role            string         `json:"role,omitempty"`
	Selected        bool           `json:"selected,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type RuntimeCapabilityProjection struct {
	TaskType          string                             `json:"task_type"`
	Status            string                             `json:"status"`
	Available         bool                               `json:"available"`
	UnavailableReason string                             `json:"unavailable_reason,omitempty"`
	ContractStatus    string                             `json:"contract_status,omitempty"`
	Reasons           []platform.RuntimeCapabilityReason `json:"reasons,omitempty"`
}

type RuntimeCapabilityError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type IntegrationVerdictDTO struct {
	SchemaVersion string                      `json:"schema_version"`
	Status        string                      `json:"status"`
	ReadyCount    int                         `json:"ready_count"`
	TotalCount    int                         `json:"total_count"`
	Gates         []IntegrationVerdictGateDTO `json:"gates"`
	Blockers      []ReadinessBlocker          `json:"blockers,omitempty"`
}

type IntegrationVerdictGateDTO struct {
	GateID   string         `json:"gate_id"`
	Label    string         `json:"label"`
	Status   string         `json:"status"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type RollbackSnapshotDTO struct {
	SchemaVersion string             `json:"schema_version"`
	SessionID     string             `json:"session_id"`
	Status        string             `json:"status"`
	Scopes        []RollbackScopeDTO `json:"scopes"`
	Instructions  []string           `json:"instructions,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

type RollbackScopeDTO struct {
	ScopeID      string         `json:"scope_id"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Action       string         `json:"action"`
	Safe         bool           `json:"safe"`
	Evidence     map[string]any `json:"evidence,omitempty"`
}

type ReleaseReadinessDTO struct {
	SchemaVersion string                      `json:"schema_version"`
	Status        string                      `json:"status"`
	Gates         []IntegrationVerdictGateDTO `json:"gates"`
	Blockers      []ReadinessBlocker          `json:"blockers,omitempty"`
}

type BusinessWorkflowDAGDTO struct {
	SchemaVersion string                    `json:"schema_version"`
	FlowID        string                    `json:"flow_id"`
	Status        string                    `json:"status"`
	Persistence   string                    `json:"persistence"`
	Nodes         []BusinessWorkflowNodeDTO `json:"nodes"`
	Edges         []BusinessWorkflowEdgeDTO `json:"edges"`
}

type BusinessWorkflowNodeDTO struct {
	NodeID    string             `json:"node_id"`
	Label     string             `json:"label"`
	Owner     string             `json:"owner"`
	Status    string             `json:"status"`
	Readiness string             `json:"readiness,omitempty"`
	Evidence  map[string]any     `json:"evidence,omitempty"`
	Blockers  []ReadinessBlocker `json:"blockers,omitempty"`
}

type BusinessWorkflowEdgeDTO struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Dependency string `json:"dependency,omitempty"`
}

type StageViewDTO struct {
	SessionID              string                        `json:"session_id"`
	ProductID              string                        `json:"product_id"`
	SKUCode                string                        `json:"sku_code"`
	ToolSlug               string                        `json:"tool_slug,omitempty"`
	TemplateID             string                        `json:"template_id,omitempty"`
	TemplateVersionID      string                        `json:"template_version_id,omitempty"`
	CurrentStage           string                        `json:"current_stage"`
	Status                 string                        `json:"status"`
	Readiness              ReadinessDTO                  `json:"readiness"`
	BusinessFlow           *BusinessWorkflowDAGDTO       `json:"business_flow,omitempty"`
	SourceReference        *SourceReferenceDTO           `json:"source_reference,omitempty"`
	SourceReferences       []SourceReferenceDTO          `json:"source_references,omitempty"`
	DeconstructionJob      *DeconstructionJobDTO         `json:"deconstruction_job,omitempty"`
	DeconstructionElements []DeconstructionElementDTO    `json:"deconstruction_elements"`
	IntentSpec             IntentSpecDTO                 `json:"intent_spec"`
	PromptPlan             PromptPlanDTO                 `json:"prompt_plan"`
	GenerationVersions     []GenerationVersionDTO        `json:"generation_versions"`
	RuntimeCapabilities    []RuntimeCapabilityProjection `json:"runtime_capabilities,omitempty"`
	RuntimeCapabilityError *RuntimeCapabilityError       `json:"runtime_capability_error,omitempty"`
	IntegrationVerdict     *IntegrationVerdictDTO        `json:"integration_verdict,omitempty"`
	RollbackSnapshot       *RollbackSnapshotDTO          `json:"rollback_snapshot,omitempty"`
	ReleaseReadiness       *ReleaseReadinessDTO          `json:"release_readiness,omitempty"`
	UpdatedAt              time.Time                     `json:"updated_at"`
}
