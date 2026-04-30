package templateutil

import "strings"

func ToolSlugFromCatalog(slug, externalCode string) string {
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

func DeriveToolSlug(route string, candidates ...string) string {
	trimmedRoute := strings.TrimSpace(route)
	if trimmedRoute != "" && !strings.Contains(trimmedRoute, "/api/") {
		trimmedRoute = strings.Trim(trimmedRoute, "/")
		if trimmedRoute != "" {
			parts := strings.Split(trimmedRoute, "/")
			last := strings.TrimSpace(parts[len(parts)-1])
			if last != "" {
				return last
			}
		}
	}
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
