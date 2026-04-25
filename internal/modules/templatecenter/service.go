package templatecenter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"ecommerce-service/internal/models"
	auditmodule "ecommerce-service/internal/modules/audit"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/pkg/logger"
	"ecommerce-service/pkg/metrics"

	"github.com/gin-gonic/gin"
)

type Service struct {
	repo     *repository.TemplateCenterRepository
	audit    *auditmodule.Service
	platform *platform.Client
}

func NewService(repo *repository.TemplateCenterRepository, audit *auditmodule.Service, platformClient *platform.Client) *Service {
	return &Service{repo: repo, audit: audit, platform: platformClient}
}

func (s *Service) SeedPresetCatalog() error {
	if err := s.repo.SeedIfEmpty(seedCatalogs()); err != nil {
		return err
	}
	summary, err := s.repo.SeedBuiltinSummary()
	if err != nil {
		return err
	}
	log := logger.With("module", "templatecenter")
	if summary.ExampleCount == 0 {
		log.Warn("template center seed summary indicates missing examples", "catalog_count", summary.CatalogCount, "version_count", summary.VersionCount, "example_count", summary.ExampleCount)
		return nil
	}
	log.Info("template center seed summary", "catalog_count", summary.CatalogCount, "version_count", summary.VersionCount, "example_count", summary.ExampleCount)
	return nil
}

func (s *Service) ListCatalog(scope repository.Scope, filter repository.TemplateCatalogFilter) ([]repository.CatalogListItem, error) {
	items, err := s.repo.ListCatalog(scope, filter)
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Facets(filter repository.TemplateCatalogFilter) (*repository.CatalogFacets, error) {
	return s.repo.ListFacets(filter)
}

func (s *Service) Recommendations(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	items, err := s.repo.ListCatalog(scope, repository.TemplateCatalogFilter{Locale: locale, FeaturedOnly: true, Limit: 8})
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Detail(scope repository.Scope, templateID, locale string) (*repository.CatalogDetail, error) {
	item, err := s.repo.GetCatalogDetail(scope, templateID, locale)
	if err != nil {
		return nil, err
	}
	s.enrichCatalogDetail(item)
	return item, nil
}

func (s *Service) Favorites(scope repository.Scope, locale string) ([]repository.CatalogListItem, error) {
	items, err := s.repo.ListFavorites(scope, locale)
	if err != nil {
		return nil, err
	}
	return s.enrichCatalogListItems(items), nil
}

func (s *Service) Instances(scope repository.Scope, locale string) ([]repository.TemplateInstanceListItem, error) {
	return s.repo.ListInstances(scope, locale)
}

func (s *Service) AddFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if err := s.repo.AddFavorite(scope, templateID); err != nil {
		return err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_favorite_total")
	_ = s.recordUsage(c, scope, templateID, "favorite", "success", nil)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.favorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template favorited"})
	}
	return nil
}

func (s *Service) RemoveFavorite(c *gin.Context, scope repository.Scope, templateID string) error {
	if err := s.repo.RemoveFavorite(scope, templateID); err != nil {
		return err
	}
	_ = s.recordUsage(c, scope, templateID, "unfavorite", "success", nil)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.unfavorite", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template unfavorited"})
	}
	return nil
}

func (s *Service) CopyToMyTemplates(c *gin.Context, scope repository.Scope, templateID string) (*models.TemplateInstance, error) {
	instance, err := s.repo.CopyToMyTemplates(scope, templateID)
	if err != nil {
		return nil, err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_copy_total")
	_ = s.recordUsage(c, scope, templateID, "copy", "success", map[string]any{"templateInstanceId": instance.ID})
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.copy_to_my_templates", TargetType: "template_instance", TargetID: instance.ID, Status: "success", Details: "template copied to my templates", AfterSnapshot: instance})
	}
	return instance, nil
}

func (s *Service) Use(c *gin.Context, scope repository.Scope, templateID string) (*repository.UseTemplateResponse, error) {
	result, err := s.repo.BuildUseResponse(scope, templateID)
	if err != nil {
		return nil, err
	}
	metrics.IncBusinessCounter("ecommerce_template_center_use_total")
	_ = s.recordUsage(c, scope, templateID, "use", "success", result)
	if s.audit != nil {
		_ = s.audit.RecordFromGin(c, auditmodule.RecordInput{Action: "template_center.use", TargetType: "template_catalog", TargetID: templateID, Status: "success", Details: "template use route resolved", AfterSnapshot: result})
	}
	return result, nil
}

func (s *Service) recordUsage(c *gin.Context, scope repository.Scope, templateID, eventType, status string, payload any) error {
	payloadJSON, _ := json.Marshal(payload)
	return s.repo.CreateUsageEvent(&models.TemplateUsageEvent{ID: buildID("tpl_evt"), EventType: eventType, TemplateCatalogID: templateID, UserID: scope.UserID, OrganizationID: scope.OrgID, RequestID: c.GetString("requestID"), TraceID: c.GetString("traceID"), RoutePath: c.FullPath(), Status: status, PayloadJSON: string(payloadJSON), CreatedAt: time.Now()})
}

func buildID(prefix string) string {
	return prefix + "_" + time.Now().Format("20060102150405.000000000")
}

func (s *Service) enrichCatalogListItems(items []repository.CatalogListItem) []repository.CatalogListItem {
	if s.platform == nil || len(items) == 0 {
		return items
	}
	sourceRefs := make([]string, 0, len(items))
	for _, item := range items {
		if item.CoverAssetURL != "" {
			continue
		}
		toolSlug := toolSlugFromCatalog(item.Slug, item.ExternalCode)
		if item.ExternalCode == "" || toolSlug == "" {
			continue
		}
		sourceRefs = append(sourceRefs, fmt.Sprintf("templates/%s/%s/example-1", toolSlug, item.ExternalCode))
	}
	resolved := s.resolveExampleAssets(sourceRefs)
	for idx := range items {
		toolSlug := toolSlugFromCatalog(items[idx].Slug, items[idx].ExternalCode)
		if items[idx].CoverAssetURL != "" || items[idx].ExternalCode == "" || toolSlug == "" {
			continue
		}
		sourceRef := fmt.Sprintf("templates/%s/%s/example-1", toolSlug, items[idx].ExternalCode)
		if asset, ok := resolved[sourceRef]; ok {
			items[idx].CoverAssetURL = buildExamplePreviewURL(asset.StorageKey)
		}
	}
	return items
}

func (s *Service) enrichCatalogDetail(item *repository.CatalogDetail) {
	if s.platform == nil || item == nil || len(item.Examples) == 0 {
		return
	}
	sourceRefs := make([]string, 0, len(item.Examples))
	for _, example := range item.Examples {
		if example.SourceRef != "" {
			sourceRefs = append(sourceRefs, example.SourceRef)
		}
	}
	resolved := s.resolveExampleAssets(sourceRefs)
	for idx := range item.Examples {
		sourceRef := item.Examples[idx].SourceRef
		if sourceRef == "" {
			continue
		}
		asset, ok := resolved[sourceRef]
		if !ok {
			continue
		}
		item.Examples[idx].StorageKey = asset.StorageKey
		item.Examples[idx].AssetID = asset.ID
		item.Examples[idx].MimeType = asset.MimeType
		item.Examples[idx].Checksum = asset.Checksum
		item.Examples[idx].PreviewAssetURL = buildExamplePreviewURL(asset.StorageKey)
	}
	if item.Catalog.CoverAssetURL == "" && len(item.Examples) > 0 && item.Examples[0].PreviewAssetURL != "" {
		item.Catalog.CoverAssetURL = item.Examples[0].PreviewAssetURL
	}
}

func (s *Service) resolveExampleAssets(sourceRefs []string) map[string]platform.AssetRecord {
	if s.platform == nil || len(sourceRefs) == 0 {
		return map[string]platform.AssetRecord{}
	}
	inputs := make([]platform.ResolveAssetInput, 0, len(sourceRefs))
	seen := map[string]struct{}{}
	for _, sourceRef := range sourceRefs {
		if sourceRef == "" {
			continue
		}
		if _, ok := seen[sourceRef]; ok {
			continue
		}
		seen[sourceRef] = struct{}{}
		inputs = append(inputs, platform.ResolveAssetInput{
			ProductCode: "ecommerce",
			Category:    "template-examples",
			SourceType:  "template_example",
			SourceRef:   sourceRef,
		})
	}
	items, err := s.platform.ResolveAssets(inputs)
	if err != nil {
		return map[string]platform.AssetRecord{}
	}
	out := make(map[string]platform.AssetRecord, len(items))
	for _, item := range items {
		out[item.SourceRef] = item
	}
	return out
}

func buildExamplePreviewURL(storageKey string) string {
	if storageKey == "" {
		return ""
	}
	return "/api/v1/ecommerce/template-center/assets/preview?storage_key=" + storageKey
}

func toolSlugFromCatalog(slug, externalCode string) string {
	if slug == "" || externalCode == "" {
		return ""
	}
	code := strings.ToLower(strings.TrimSpace(externalCode))
	marker := "-" + code + "-"
	if idx := strings.Index(slug, marker); idx > 0 {
		return slug[:idx]
	}
	return ""
}

func (s *Service) DownloadExampleAsset(storageKey string) (io.ReadCloser, map[string]string, error) {
	if s.platform == nil {
		return nil, nil, fmt.Errorf("platform client is not configured")
	}
	body, header, err := s.platform.DownloadAsset(storageKey)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	if contentType := header.Get("Content-Type"); contentType != "" {
		out["Content-Type"] = contentType
	}
	if cacheControl := header.Get("Cache-Control"); cacheControl != "" {
		out["Cache-Control"] = cacheControl
	}
	if contentLength := header.Get("Content-Length"); contentLength != "" {
		out["Content-Length"] = contentLength
	}
	return body, out, nil
}
