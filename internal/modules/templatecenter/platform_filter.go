package templatecenter

import (
	"sort"
	"strings"

	"ecommerce-service/internal/repository"
)

func matchesPlatformCatalogFilter(item repository.CatalogListItem, filter repository.TemplateCatalogFilter) bool {
	if filter.ToolSlug != "" && !strings.EqualFold(item.ToolSlug, filter.ToolSlug) {
		return false
	}
	if filter.Modality != "" && !strings.EqualFold(item.Modality, filter.Modality) {
		return false
	}
	if filter.Series != "" && !strings.EqualFold(item.Series, filter.Series) {
		return false
	}
	if filter.Capability != "" && !strings.EqualFold(item.CapabilityType, filter.Capability) {
		return false
	}
	if filter.Platform != "" && !containsFold(item.PlatformTags, filter.Platform) {
		return false
	}
	if filter.InputMode != "" && len(item.InputModes) > 0 && !containsFold(item.InputModes, filter.InputMode) {
		return false
	}
	if filter.ProductCategory != "" {
		if containsFold(stringSliceFromAny(firstAnyValue(item.Applicability, "product_category_exclude", "productCategoryExclude", "product_categories_exclude", "productCategoriesExclude")), filter.ProductCategory) {
			return false
		}
		if len(item.ProductCategories) > 0 && !containsFold(item.ProductCategories, filter.ProductCategory) {
			return false
		}
	}
	if filter.ProviderCapability != "" && len(item.ProviderCapabilities) > 0 && !containsFold(item.ProviderCapabilities, filter.ProviderCapability) {
		return false
	}
	if filter.Industry != "" && !containsFold(item.IndustryTags, filter.Industry) {
		return false
	}
	if filter.Scenario != "" && !containsFold(item.ScenarioTags, filter.Scenario) {
		return false
	}
	if filter.FeaturedOnly && !item.IsFeatured {
		return false
	}
	keyword := strings.TrimSpace(strings.ToLower(filter.Keyword))
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Name), keyword) ||
		strings.Contains(strings.ToLower(item.Summary), keyword) ||
		strings.Contains(strings.ToLower(item.ExternalCode), keyword) ||
		strings.Contains(strings.ToLower(item.ID), keyword)
}

func sortPlatformCatalogItems(items []repository.CatalogListItem, sortBy string) {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "latest":
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	case "name":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].RecommendScore == items[j].RecommendScore {
				return items[i].Name < items[j].Name
			}
			return items[i].RecommendScore > items[j].RecommendScore
		})
	}
}

func containsFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}
