package auth

import (
	"fmt"
	"strings"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/modules/authz"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
)

type Service struct {
	platform *platform.Client
	users    *repository.UserRepository
	authz    *authz.Service
	appCfg   config.AppConfig
}

type RegisterInput struct {
	FullName         string `json:"full_name" binding:"required,min=2"`
	Email            string `json:"email" binding:"required,email"`
	Password         string `json:"password" binding:"required,min=6"`
	OrganizationName string `json:"organization_name,omitempty"`
	Language         string `json:"language,omitempty"`
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type UserSummary struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	FullName           string `json:"full_name"`
	AvatarURL          string `json:"avatar_url"`
	OrgID              string `json:"org_id"`
	OrgName            string `json:"org_name"`
	OrgRole            string `json:"org_role"`
	PlanID             string `json:"plan_id"`
	Status             string `json:"status"`
	LanguagePreference string `json:"language_preference,omitempty"`
}

type CreditsSummary struct {
	AssetCode        string `json:"asset_code"`
	Balance          int64  `json:"balance"`
	PermanentBalance int64  `json:"permanent_balance"`
	RewardBalance    int64  `json:"reward_balance"`
	AllowanceBalance int64  `json:"allowance_balance"`
}

type AuthResult struct {
	AccessToken string              `json:"access_token"`
	User        UserSummary         `json:"user"`
	Credits     CreditsSummary      `json:"credits"`
	Access      authz.AccessSummary `json:"access"`
}

type SessionResult struct {
	Authenticated bool                `json:"authenticated"`
	User          UserSummary         `json:"user"`
	Credits       CreditsSummary      `json:"credits"`
	Access        authz.AccessSummary `json:"access"`
}

func NewService(platformClient *platform.Client, userRepo *repository.UserRepository, authzService *authz.Service, appCfg config.AppConfig) *Service {
	return &Service{platform: platformClient, users: userRepo, authz: authzService, appCfg: appCfg}
}

func (s *Service) Register(input RegisterInput) (*AuthResult, error) {
	orgName := strings.TrimSpace(input.OrganizationName)
	if orgName == "" {
		orgName = strings.TrimSpace(input.FullName) + "'s Workspace"
	}
	out, err := s.platform.Register(platform.AuthRegisterInput{FullName: input.FullName, Email: input.Email, Password: input.Password, Company: orgName})
	if err != nil {
		return nil, err
	}
	language := defaultString(input.Language, s.appCfg.DefaultLanguage)
	if s.users != nil {
		_, _ = s.users.UpsertPreference(out.User.ID, out.User.OrgID, language)
		_ = s.users.CreateActivity(&models.Activity{UserID: out.User.ID, OrganizationID: out.User.OrgID, ActionType: "auth_register", ActionName: "Product Register", Status: "succeeded", EventID: out.User.ID})
	}
	access, err := s.authz.Resolve(out.User.ID, out.User.OrgID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{AccessToken: out.AccessToken, User: s.buildUserSummary(out.User), Credits: s.buildCreditsSummary(out.User.OrgID), Access: *access}, nil
}

func (s *Service) Login(input LoginInput) (*AuthResult, error) {
	out, err := s.platform.Login(platform.AuthLoginInput{Email: input.Email, Password: input.Password})
	if err != nil {
		return nil, err
	}
	if s.users != nil {
		_ = s.users.CreateActivity(&models.Activity{UserID: out.User.ID, OrganizationID: out.User.OrgID, ActionType: "auth_login", ActionName: "Product Login", Status: "succeeded", EventID: out.User.ID})
	}
	access, err := s.authz.Resolve(out.User.ID, out.User.OrgID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{AccessToken: out.AccessToken, User: s.buildUserSummary(out.User), Credits: s.buildCreditsSummary(out.User.OrgID), Access: *access}, nil
}

func (s *Service) Session(userID, orgID string) (*SessionResult, error) {
	profile, err := s.platform.GetUserProfile(userID, orgID)
	if err != nil {
		return nil, err
	}
	access, err := s.authz.Resolve(profile.ID, profile.OrgID)
	if err != nil {
		return nil, err
	}
	return &SessionResult{Authenticated: true, User: s.buildUserSummary(*profile), Credits: s.buildCreditsSummary(profile.OrgID), Access: *access}, nil
}

func (s *Service) buildUserSummary(user platform.PlatformUserProfile) UserSummary {
	return UserSummary{ID: user.ID, Email: user.Email, FullName: user.FullName, AvatarURL: user.AvatarURL, OrgID: user.OrgID, OrgName: currentOrgName(user), OrgRole: user.OrgRole, PlanID: user.PlanID, Status: user.Status, LanguagePreference: s.lookupLanguagePreference(user.ID, user.OrgID)}
}

func (s *Service) buildCreditsSummary(orgID string) CreditsSummary {
	summary, err := s.platform.GetWalletSummary("organization", orgID, s.appCfg.ProductCode)
	if err != nil || summary == nil {
		return CreditsSummary{AssetCode: s.appCfg.ProductCode + "_CREDIT"}
	}
	return CreditsSummary{AssetCode: s.appCfg.ProductCode + "_CREDIT", Balance: summary.TotalBalance, PermanentBalance: summary.PermanentBalance, RewardBalance: summary.RewardBalance, AllowanceBalance: summary.AllowanceBalance}
}

func (s *Service) lookupLanguagePreference(userID, orgID string) string {
	if s.users == nil {
		return s.appCfg.DefaultLanguage
	}
	item, err := s.users.GetPreference(userID, orgID)
	if err != nil || item.LanguagePreference == "" {
		return s.appCfg.DefaultLanguage
	}
	return item.LanguagePreference
}

func currentOrgName(user platform.PlatformUserProfile) string {
	for _, org := range user.Orgs {
		if org.ID == user.OrgID {
			return org.Name
		}
	}
	return ""
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
func (_ *Service) String() string { return fmt.Sprintf("auth-service") }
