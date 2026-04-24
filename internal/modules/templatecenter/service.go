package templatecenter

import (
	"encoding/json"
	"time"

	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
	"ecommerce-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

type Service struct {
	repo  *repository.TemplateCenterRepository
	audit *auditmodule.Service
}

func NewService(repo *repository.TemplateCenterRepository, audit *auditmodule.Service) *Service {
	return &Service{repo: repo, audit: audit}
}

func (s *Service) SeedPresetCatalog() error {
	return s.repo.SeedIfEmpty(seedCatalogs())
}

func (s *Service) ListCatalog(scope repository.Scope, filter repository.TemplateCatalogFilter) ([]repository.CatalogListItem, error) {
	return s.repo.ListCatalog(scope, filter)
}

func (s *Service) Facets(filter repository.TemplateCatalogFilter) (*repository.CatalogFacets, error) {
	return s.repo.ListFacets(filter)
}

func (s *Service) Recommendations(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	return s.repo.ListCatalog(scope, repository.TemplateCatalogFilter{Locale: locale, FeaturedOnly: true, Limit: 8})
}

func (s *Service) Detail(scope repository.Scope, templateID, locale string) (*repository.CatalogDetail, error) {
	return s.repo.GetCatalogDetail(scope, templateID, locale)
}

func (s *Service) Favorites(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	return s.repo.ListFavorites(scope, locale)
}

func (s *Service) Instances(scope repository.Scope, locale string) ([]repository.TemplateInstanceListItem, error) {
	return s.repo.ListInstances(scope, locale)
}

func (s *Service) AddFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if err := s.repo.AddFavorite(scope, templateID); err != nil { return err }
	metrics.IncBusinessCounter("ecommerce_template_center_favorite_total")
	_ = s.recordUsage(c, scope, templateID, "favorite", "success", nil)
	if s.audit != nil { _ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.favorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template favorited"}) }
	return nil
}

func (s *Service) RemoveFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if err := s.repo.RemoveFavorite(scope, templateID); err != nil { return err }
	_ = s.recordUsage(c, scope, templateID, "unfavorite", "success", nil)
	if s.audit != nil { _ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.unfavorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template unfavorited"}) }
	return nil
}

func (s *Service) CopyToMyTemplates(c *gin.Context, scope repository.Scope, templateID string) (*models.TemplateInstance, error) {
	instance, err := s.repo.CopyToMyTemplates(scope, templateID)
	if err != nil { return nil, err }
	metrics.IncBusinessCounter("ecommerce_template_center_copy_total")
	_ = s.recordUsage(c, scope, templateID, "copy", "success", map[string]any{"templateInstanceId": instance.ID})
	if s.audit != nil { _ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.copy_to_my_templates", TargetType: "template_instance", TargetID: instance.ID, Status: "success", Details: "template copied to my templates", AfterSnapshot: instance}) }
	return instance, nil
}

func (s *Service) Use(c *gin.Context, scope repository.Scope, templateID string) (*repository.UseTemplateResponse, error) {
	result, err := s.repo.BuildUseResponse(scope, templateID)
	if err != nil { return nil, err }
	metrics.IncBusinessCounter("ecommerce_template_center_use_total")
	_ = s.recordUsage(c, scope, templateID, "use", "success", result)
	if s.audit != nil { _ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.use", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template use route resolved", AfterSnapshot: result}) }
	return result, nil
}

func (s *Service) recordUsage(c *gin.Context, scope repository.Scope, templateID, eventType, status string, payload any) error {
	payloadJSON, _ := json.Marshal(payload)
	return s.repo.CreateUsageEvent(&models.TemplateUsageEvent{ID: buildID("tpl_evt"), EventType: eventType, TemplateCatalogID: templateID, UserID: scope.UserID, OrganizationID: scope.OrgID, RequestID: c.GetString("requestID"), TraceID: c.GetString("traceID"), RoutePath: c.FullPath(), Status: status, PayloadJSON: string(payloadJSON), CreatedAt: time.Now()})
}

func buildID(prefix string) string { return prefix + "_" + time.Now().Format("20060102150405.000000000") }
