package visualworkflow

import (
	"encoding/json"
	"time"

	"ecommerce-service/internal/platform"
)

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
