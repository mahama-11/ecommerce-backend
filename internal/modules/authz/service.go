package authz

import "ecommerce-service/internal/platform"

type Service struct{ platform *platform.Client }

type AccessSummary struct {
	ActiveOrgID         string   `json:"active_org_id"`
	HasAccess           bool     `json:"has_access"`
	ProductRoles        []string `json:"product_roles"`
	ProductPermissions  []string `json:"product_permissions"`
	PlatformPermissions []string `json:"platform_permissions,omitempty"`
}

func NewService(platformClient *platform.Client) *Service { return &Service{platform: platformClient} }

func (s *Service) Resolve(userID, orgID string) (*AccessSummary, error) {
	ctx, err := s.platform.GetAccessContext(userID, orgID)
	if err != nil {
		return nil, err
	}
	roles, permissions := defaultAccessByOrgRole(ctx.OrgRole)
	return &AccessSummary{ActiveOrgID: ctx.OrgID, HasAccess: true, ProductRoles: roles, ProductPermissions: permissions, PlatformPermissions: ctx.Permissions}, nil
}

func defaultAccessByOrgRole(orgRole string) ([]string, []string) {
	switch orgRole {
	case "owner", "admin":
		return []string{"ecommerce.workspace_admin"}, []string{"ecommerce.access", "ecommerce.template.read", "ecommerce.template.save", "ecommerce.workflow.read", "ecommerce.workflow.write", "ecommerce.asset.read", "ecommerce.asset.write"}
	case "viewer":
		return []string{"ecommerce.viewer"}, []string{"ecommerce.access", "ecommerce.template.read", "ecommerce.workflow.read", "ecommerce.asset.read"}
	default:
		return []string{"ecommerce.editor"}, []string{"ecommerce.access", "ecommerce.template.read", "ecommerce.template.save", "ecommerce.workflow.read", "ecommerce.workflow.write", "ecommerce.asset.read", "ecommerce.asset.write"}
	}
}
