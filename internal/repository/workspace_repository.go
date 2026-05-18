package repository

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type Scope struct {
	UserID string
	OrgID  string
}

type LocalizedText struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

type TemplateCopy struct {
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Scenario string `json:"scenario"`
}

type SavedTemplateRecord struct {
	ID          string       `json:"id"`
	Platform    string       `json:"platform"`
	Tags        []string     `json:"tags"`
	UsageCount  string       `json:"usageCount"`
	Favorite    float64      `json:"favorite"`
	SavedAt     string       `json:"savedAt"`
	SourceType  string       `json:"sourceType,omitempty"`
	SourceLabel string       `json:"sourceLabel,omitempty"`
	ZH          TemplateCopy `json:"zh"`
	EN          TemplateCopy `json:"en"`
}

type WorkflowEventRecord struct {
	ID        string        `json:"id"`
	Module    string        `json:"module"`
	Title     LocalizedText `json:"title"`
	Detail    LocalizedText `json:"detail"`
	CreatedAt string        `json:"createdAt"`
}

type LinkedDesignAssetRecord struct {
	ID         string        `json:"id"`
	SourcePath string        `json:"sourcePath"`
	Title      LocalizedText `json:"title"`
	Desc       LocalizedText `json:"desc"`
	SyncedAt   string        `json:"syncedAt"`
}

type LinkedDeliveryRecord struct {
	ID         string        `json:"id"`
	SourcePath string        `json:"sourcePath"`
	Title      LocalizedText `json:"title"`
	Size       string        `json:"size"`
	Status     string        `json:"status"`
	Meta       LocalizedText `json:"meta"`
	CreatedAt  string        `json:"createdAt"`
}

type LinkedTemplateBridgeRecord struct {
	ID              string        `json:"id"`
	DesignTitle     LocalizedText `json:"designTitle"`
	AITemplateTitle LocalizedText `json:"aiTemplateTitle"`
	Scenario        LocalizedText `json:"scenario"`
	CreatedAt       string        `json:"createdAt"`
}

type WorkspaceRepository struct{ db *gorm.DB }

func NewWorkspaceRepository(db *gorm.DB) *WorkspaceRepository { return &WorkspaceRepository{db: db} }

func (r *WorkspaceRepository) ListSavedTemplates(scope Scope) ([]SavedTemplateRecord, error) {
	var items []models.SavedTemplate
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]SavedTemplateRecord, 0, len(items))
	for _, item := range items {
		out = append(out, toSavedTemplateRecord(item))
	}
	return out, nil
}

func (r *WorkspaceRepository) SaveTemplate(scope Scope, record SavedTemplateRecord) ([]SavedTemplateRecord, error) {
	model := fromSavedTemplateRecord(scope, record)
	model.UpdatedAt = time.Now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", model.ID, scope.UserID, scope.OrgID).Assign(model).FirstOrCreate(&models.SavedTemplate{}).Error; err != nil {
		return nil, err
	}
	return r.ListSavedTemplates(scope)
}

func (r *WorkspaceRepository) DeleteTemplate(scope Scope, id string) ([]SavedTemplateRecord, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("template id is required")
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", strings.TrimSpace(id), scope.UserID, scope.OrgID).Delete(&models.SavedTemplate{}).Error; err != nil {
		return nil, err
	}
	return r.ListSavedTemplates(scope)
}

func (r *WorkspaceRepository) MarkTemplateUsed(scope Scope, id string) (*SavedTemplateRecord, error) {
	var item models.SavedTemplate
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", strings.TrimSpace(id), scope.UserID, scope.OrgID).First(&item).Error; err != nil {
		return nil, err
	}
	current, _ := strconv.Atoi(strings.TrimSpace(item.UsageCount))
	item.UsageCount = strconv.Itoa(current + 1)
	item.UpdatedAt = time.Now()
	if err := r.db.Save(&item).Error; err != nil {
		return nil, err
	}
	out := toSavedTemplateRecord(item)
	return &out, nil
}

func (r *WorkspaceRepository) ListWorkflowEvents(scope Scope) ([]WorkflowEventRecord, error) {
	var items []models.WorkflowEvent
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]WorkflowEventRecord, 0, len(items))
	for _, item := range items {
		out = append(out, toWorkflowEventRecord(item))
	}
	return out, nil
}

func (r *WorkspaceRepository) SaveWorkflowEvent(scope Scope, record WorkflowEventRecord) ([]WorkflowEventRecord, error) {
	model := fromWorkflowEventRecord(scope, record)
	model.UpdatedAt = time.Now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", model.ID, scope.UserID, scope.OrgID).Assign(model).FirstOrCreate(&models.WorkflowEvent{}).Error; err != nil {
		return nil, err
	}
	return r.ListWorkflowEvents(scope)
}

func (r *WorkspaceRepository) ListLinkedDesignAssets(scope Scope) ([]LinkedDesignAssetRecord, error) {
	var items []models.LinkedDesignAsset
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]LinkedDesignAssetRecord, 0, len(items))
	for _, item := range items {
		out = append(out, toLinkedDesignAssetRecord(item))
	}
	return out, nil
}

func (r *WorkspaceRepository) SaveLinkedDesignAsset(scope Scope, record LinkedDesignAssetRecord) ([]LinkedDesignAssetRecord, error) {
	model := fromLinkedDesignAssetRecord(scope, record)
	model.UpdatedAt = time.Now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", model.ID, scope.UserID, scope.OrgID).Assign(model).FirstOrCreate(&models.LinkedDesignAsset{}).Error; err != nil {
		return nil, err
	}
	return r.ListLinkedDesignAssets(scope)
}

func (r *WorkspaceRepository) ListLinkedDeliveries(scope Scope) ([]LinkedDeliveryRecord, error) {
	var items []models.LinkedDelivery
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]LinkedDeliveryRecord, 0, len(items))
	for _, item := range items {
		out = append(out, toLinkedDeliveryRecord(item))
	}
	return out, nil
}

func (r *WorkspaceRepository) SaveLinkedDelivery(scope Scope, record LinkedDeliveryRecord) ([]LinkedDeliveryRecord, error) {
	model := fromLinkedDeliveryRecord(scope, record)
	model.UpdatedAt = time.Now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", model.ID, scope.UserID, scope.OrgID).Assign(model).FirstOrCreate(&models.LinkedDelivery{}).Error; err != nil {
		return nil, err
	}
	return r.ListLinkedDeliveries(scope)
}

func (r *WorkspaceRepository) ListTemplateBridges(scope Scope) ([]LinkedTemplateBridgeRecord, error) {
	var items []models.TemplateBridge
	if err := r.db.Where("user_id = ? AND organization_id = ?", scope.UserID, scope.OrgID).Order("updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]LinkedTemplateBridgeRecord, 0, len(items))
	for _, item := range items {
		out = append(out, toTemplateBridgeRecord(item))
	}
	return out, nil
}

func (r *WorkspaceRepository) SaveTemplateBridge(scope Scope, record LinkedTemplateBridgeRecord) ([]LinkedTemplateBridgeRecord, error) {
	model := fromTemplateBridgeRecord(scope, record)
	model.UpdatedAt = time.Now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if err := r.db.Where("id = ? AND user_id = ? AND organization_id = ?", model.ID, scope.UserID, scope.OrgID).Assign(model).FirstOrCreate(&models.TemplateBridge{}).Error; err != nil {
		return nil, err
	}
	return r.ListTemplateBridges(scope)
}

func toSavedTemplateRecord(item models.SavedTemplate) SavedTemplateRecord {
	var tags []string
	_ = json.Unmarshal([]byte(item.TagsJSON), &tags)
	return SavedTemplateRecord{ID: item.ID, Platform: item.Platform, Tags: tags, UsageCount: item.UsageCount, Favorite: item.Favorite, SavedAt: item.SavedAt, SourceType: item.SourceType, SourceLabel: item.SourceLabel, ZH: TemplateCopy{Title: item.ZHTitle, Summary: item.ZHSummary, Scenario: item.ZHScenario}, EN: TemplateCopy{Title: item.ENTitle, Summary: item.ENSummary, Scenario: item.ENScenario}}
}

func fromSavedTemplateRecord(scope Scope, record SavedTemplateRecord) models.SavedTemplate {
	tags, _ := json.Marshal(record.Tags)
	return models.SavedTemplate{ID: record.ID, UserID: scope.UserID, OrganizationID: scope.OrgID, Platform: record.Platform, TagsJSON: string(tags), UsageCount: record.UsageCount, Favorite: record.Favorite, SavedAt: record.SavedAt, SourceType: record.SourceType, SourceLabel: record.SourceLabel, ZHTitle: record.ZH.Title, ZHSummary: record.ZH.Summary, ZHScenario: record.ZH.Scenario, ENTitle: record.EN.Title, ENSummary: record.EN.Summary, ENScenario: record.EN.Scenario}
}

func toWorkflowEventRecord(item models.WorkflowEvent) WorkflowEventRecord {
	return WorkflowEventRecord{ID: item.ID, Module: item.Module, Title: LocalizedText{ZH: item.TitleZH, EN: item.TitleEN}, Detail: LocalizedText{ZH: item.DetailZH, EN: item.DetailEN}, CreatedAt: item.CreatedAtISO}
}

func fromWorkflowEventRecord(scope Scope, record WorkflowEventRecord) models.WorkflowEvent {
	return models.WorkflowEvent{ID: record.ID, UserID: scope.UserID, OrganizationID: scope.OrgID, Module: record.Module, TitleZH: record.Title.ZH, TitleEN: record.Title.EN, DetailZH: record.Detail.ZH, DetailEN: record.Detail.EN, CreatedAtISO: record.CreatedAt}
}

func toLinkedDesignAssetRecord(item models.LinkedDesignAsset) LinkedDesignAssetRecord {
	return LinkedDesignAssetRecord{ID: item.ID, SourcePath: item.SourcePath, Title: LocalizedText{ZH: item.TitleZH, EN: item.TitleEN}, Desc: LocalizedText{ZH: item.DescZH, EN: item.DescEN}, SyncedAt: item.SyncedAt}
}

func fromLinkedDesignAssetRecord(scope Scope, record LinkedDesignAssetRecord) models.LinkedDesignAsset {
	return models.LinkedDesignAsset{ID: record.ID, UserID: scope.UserID, OrganizationID: scope.OrgID, SourcePath: record.SourcePath, TitleZH: record.Title.ZH, TitleEN: record.Title.EN, DescZH: record.Desc.ZH, DescEN: record.Desc.EN, SyncedAt: record.SyncedAt}
}

func toLinkedDeliveryRecord(item models.LinkedDelivery) LinkedDeliveryRecord {
	return LinkedDeliveryRecord{ID: item.ID, SourcePath: item.SourcePath, Title: LocalizedText{ZH: item.TitleZH, EN: item.TitleEN}, Size: item.Size, Status: item.Status, Meta: LocalizedText{ZH: item.MetaZH, EN: item.MetaEN}, CreatedAt: item.CreatedAtISO}
}

func fromLinkedDeliveryRecord(scope Scope, record LinkedDeliveryRecord) models.LinkedDelivery {
	return models.LinkedDelivery{ID: record.ID, UserID: scope.UserID, OrganizationID: scope.OrgID, SourcePath: record.SourcePath, TitleZH: record.Title.ZH, TitleEN: record.Title.EN, Size: record.Size, Status: record.Status, MetaZH: record.Meta.ZH, MetaEN: record.Meta.EN, CreatedAtISO: record.CreatedAt}
}

func toTemplateBridgeRecord(item models.TemplateBridge) LinkedTemplateBridgeRecord {
	return LinkedTemplateBridgeRecord{ID: item.ID, DesignTitle: LocalizedText{ZH: item.DesignTitleZH, EN: item.DesignTitleEN}, AITemplateTitle: LocalizedText{ZH: item.AITitleZH, EN: item.AITitleEN}, Scenario: LocalizedText{ZH: item.ScenarioZH, EN: item.ScenarioEN}, CreatedAt: item.CreatedAtISO}
}

func fromTemplateBridgeRecord(scope Scope, record LinkedTemplateBridgeRecord) models.TemplateBridge {
	return models.TemplateBridge{ID: record.ID, UserID: scope.UserID, OrganizationID: scope.OrgID, DesignTitleZH: record.DesignTitle.ZH, DesignTitleEN: record.DesignTitle.EN, AITitleZH: record.AITemplateTitle.ZH, AITitleEN: record.AITemplateTitle.EN, ScenarioZH: record.Scenario.ZH, ScenarioEN: record.Scenario.EN, CreatedAtISO: record.CreatedAt}
}
