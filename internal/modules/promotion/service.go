package promotion

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"
	"ecommerce-service/pkg/logger"
)

type Service struct {
	platform        *platform.Client
	repo            *repository.CommercialRepository
	frontendBaseURL string
	productCode     string
	creditsAsset    string
	rewardAsset     string
	allowanceAsset  string
	programCode     string
	programName     string
}

type PromotionCodeSummary struct {
	ID          string         `json:"id"`
	ProgramID   string         `json:"program_id"`
	ProductCode string         `json:"product_code"`
	Code        string         `json:"code"`
	Status      string         `json:"status"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	InviteURL   string         `json:"invite_url,omitempty"`
	SignupURL   string         `json:"signup_url,omitempty"`
	ShareText   string         `json:"share_text,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

type PromotionOverview struct {
	Programs            []platform.ReferralProgram    `json:"programs"`
	Codes               []PromotionCodeSummary        `json:"codes"`
	Conversions         []platform.ReferralConversion `json:"conversions"`
	TotalConversions    int                           `json:"total_conversions"`
	TrackedConversions  int                           `json:"tracked_conversions"`
	EarnedConversions   int                           `json:"earned_conversions"`
	ReversedConversions int                           `json:"reversed_conversions"`
	InviteBaseURL       string                        `json:"invite_base_url,omitempty"`
}

type CreateCodeInput struct {
	ProgramCode string `json:"program_code"`
	Code        string `json:"code"`
	Metadata    string `json:"metadata"`
}

func NewService(platformClient *platform.Client, repo *repository.CommercialRepository, appCfg config.AppConfig) *Service {
	return &Service{
		platform:        platformClient,
		repo:            repo,
		frontendBaseURL: strings.TrimRight(appCfg.FrontendBaseURL, "/"),
		productCode:     defaultString(appCfg.ProductCode, "ecommerce"),
		creditsAsset:    defaultString(appCfg.CreditsAssetCode, "ECOMMERCE_CREDIT"),
		rewardAsset:     defaultString(appCfg.RewardAssetCode, "ECOMMERCE_PROMO_CREDIT"),
		allowanceAsset:  defaultString(appCfg.AllowanceAssetCode, "ECOMMERCE_MONTHLY_ALLOWANCE"),
		programCode:     defaultString(appCfg.PromotionProgramCode, "ecommerce_signup_default"),
		programName:     defaultString(appCfg.PromotionProgramName, "Ecommerce Signup Promotion"),
	}
}

func (s *Service) Bootstrap() error {
	if err := s.bootstrapAssetDefinitions(); err != nil {
		return err
	}
	return s.bootstrapProgram()
}

func (s *Service) ListPrograms(status string) ([]platform.ReferralProgram, error) {
	items, err := s.platform.ListReferralPrograms(s.productCode, defaultString(status, "active"))
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProgramCode < items[j].ProgramCode })
	return items, nil
}

func (s *Service) ListCodes(orgID, programCode, status string) ([]PromotionCodeSummary, error) {
	programID, err := s.resolveProgramID(programCode)
	if err != nil {
		return nil, err
	}
	items, err := s.platform.ListReferralCodes(programID, "organization", orgID, status)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	out := make([]PromotionCodeSummary, 0, len(items))
	for _, item := range items {
		out = append(out, s.mapCode(item))
	}
	return out, nil
}

func (s *Service) CreateCode(orgID string, input CreateCodeInput) (*PromotionCodeSummary, error) {
	programCode := defaultString(strings.TrimSpace(input.ProgramCode), s.programCode)
	item, err := s.platform.CreateReferralCode(platform.CreateReferralCodeInput{
		ProgramCode:         programCode,
		Code:                strings.TrimSpace(input.Code),
		PromoterSubjectType: "organization",
		PromoterSubjectID:   orgID,
		Status:              "active",
		Metadata:            input.Metadata,
	})
	if err != nil {
		return nil, err
	}
	mapped := s.mapCode(*item)
	return &mapped, nil
}

func (s *Service) EnsureCode(orgID string, input CreateCodeInput) (*PromotionCodeSummary, error) {
	items, err := s.ListCodes(orgID, defaultString(strings.TrimSpace(input.ProgramCode), s.programCode), "active")
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return &items[0], nil
	}
	return s.CreateCode(orgID, input)
}

func (s *Service) ResolveCode(code string) (*platform.ResolvedReferralCode, error) {
	return s.platform.ResolveReferralCode(code, s.productCode)
}

func (s *Service) ListConversions(orgID, status string) ([]platform.ReferralConversion, error) {
	items, err := s.platform.ListReferralConversions(s.productCode, "organization", orgID, status)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) Overview(orgID, conversionStatus string) (*PromotionOverview, error) {
	programs, err := s.ListPrograms("active")
	if err != nil {
		return nil, err
	}
	codes, err := s.ListCodes(orgID, "", "")
	if err != nil {
		return nil, err
	}
	conversions, err := s.ListConversions(orgID, conversionStatus)
	if err != nil {
		return nil, err
	}
	out := &PromotionOverview{
		Programs:      programs,
		Codes:         codes,
		Conversions:   conversions,
		InviteBaseURL: s.frontendBaseURL,
	}
	for _, item := range conversions {
		out.TotalConversions++
		switch item.Status {
		case "commission_earned", "reward_issued":
			out.EarnedConversions++
		case "reversed":
			out.ReversedConversions++
		default:
			out.TrackedConversions++
		}
	}
	return out, nil
}

func (s *Service) TrackSignupAttribution(orgID, userID, promotionCode string) {
	code := strings.TrimSpace(promotionCode)
	if code == "" || s.repo == nil {
		return
	}
	attempt := &models.PromotionAttributionAttempt{
		ProductCode:    s.productCode,
		OrganizationID: orgID,
		UserID:         userID,
		PromotionCode:  code,
		TriggerType:    "signup",
		ReferenceType:  "auth_register",
		ReferenceID:    orgID,
		Status:         "pending",
	}
	if err := s.repo.CreatePromotionAttempt(attempt); err != nil {
		logger.With("org_id", orgID, "user_id", userID, "promotion_code", code, "error", err).Error("promotion.attribution_attempt.persist_failed")
		return
	}
	conversion, err := s.platform.CreateReferralConversion(platform.CreateReferralConversionInput{
		ReferralCode:          code,
		ProductCode:           s.productCode,
		TriggerType:           "signup",
		ReferredSubjectType:   "organization",
		ReferredSubjectID:     orgID,
		SettlementSubjectType: "organization",
		SettlementSubjectID:   orgID,
		ReferenceType:         "auth_register",
		ReferenceID:           orgID,
		CommissionCurrency:    s.creditsAsset,
		Metadata:              fmt.Sprintf(`{"user_id":%q}`, userID),
	})
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorCode = platform.ErrorCode(err)
		attempt.ErrorMessage = err.Error()
		if updateErr := s.repo.UpdatePromotionAttempt(attempt); updateErr != nil {
			logger.With("attempt_id", attempt.ID, "error", updateErr).Error("promotion.attribution_attempt.update_failed")
		}
		logger.With("org_id", orgID, "user_id", userID, "promotion_code", code, "error", err).Warn("promotion.signup_attribution.failed")
		return
	}
	attempt.Status = "succeeded"
	attempt.PlatformConversionID = conversion.ID
	attempt.MetadataJSON = conversion.Metadata
	if err := s.repo.UpdatePromotionAttempt(attempt); err != nil {
		logger.With("attempt_id", attempt.ID, "error", err).Error("promotion.attribution_attempt.update_failed")
	}
}

func (s *Service) mapCode(item platform.ReferralCode) PromotionCodeSummary {
	signupURL := s.buildSignupURL(item.Code)
	return PromotionCodeSummary{
		ID:          item.ID,
		ProgramID:   item.ProgramID,
		ProductCode: item.ProductCode,
		Code:        item.Code,
		Status:      item.Status,
		Metadata:    decodeStringMap(item.Metadata),
		InviteURL:   signupURL,
		SignupURL:   signupURL,
		ShareText:   s.buildShareText(item.Code, signupURL),
		CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Service) buildSignupURL(code string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}
	base := defaultString(s.frontendBaseURL, "http://localhost:5180")
	return fmt.Sprintf("%s/signup?promotion_code=%s", base, url.QueryEscape(code))
}

func (s *Service) buildShareText(code, signupURL string) string {
	if signupURL == "" {
		return ""
	}
	return fmt.Sprintf("Use my ecommerce invite code %s to join: %s", code, signupURL)
}

func (s *Service) bootstrapAssetDefinitions() error {
	existing, err := s.platform.ListAssetDefinitions(s.productCode, "", "")
	if err != nil {
		return err
	}
	byCode := make(map[string]platform.AssetDefinition, len(existing))
	for _, item := range existing {
		byCode[item.AssetCode] = item
	}
	definitions := []platform.CreateAssetDefinitionInput{
		{
			AssetCode:     s.creditsAsset,
			ProductCode:   s.productCode,
			AssetType:     "cash_balance",
			LifecycleType: "permanent",
			Status:        "active",
			Description:   "Ecommerce permanent wallet balance",
			Metadata:      `{"seeded_by":"ecommerce_bootstrap","asset_role":"primary_balance"}`,
		},
		{
			AssetCode:         s.rewardAsset,
			ProductCode:       s.productCode,
			AssetType:         "reward_credit",
			LifecycleType:     "expiring",
			DefaultExpireDays: 30,
			Status:            "active",
			Description:       "Ecommerce promotional reward credit",
			Metadata:          `{"seeded_by":"ecommerce_bootstrap","asset_role":"promo_reward"}`,
		},
		{
			AssetCode:     s.allowanceAsset,
			ProductCode:   s.productCode,
			AssetType:     "subscription_allowance",
			LifecycleType: "cycle_reset",
			ResetCycle:    "monthly",
			Status:        "active",
			Description:   "Ecommerce monthly allowance",
			Metadata:      `{"seeded_by":"ecommerce_bootstrap","asset_role":"monthly_allowance"}`,
		},
	}
	for _, item := range definitions {
		if _, ok := byCode[item.AssetCode]; ok {
			continue
		}
		if _, err := s.platform.CreateAssetDefinition(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) bootstrapProgram() error {
	items, err := s.platform.ListReferralPrograms(s.productCode, "")
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ProgramCode == s.programCode {
			return nil
		}
	}
	_, err = s.platform.CreateReferralProgram(platform.CreateReferralProgramInput{
		ProductCode:           s.productCode,
		ProgramCode:           s.programCode,
		Name:                  s.programName,
		TriggerType:           "signup",
		CommissionPolicy:      "fixed_amount",
		CommissionCurrency:    s.creditsAsset,
		CommissionFixedAmount: 20,
		SettlementDelayDays:   0,
		AllowRepeat:           false,
		Status:                "active",
		Metadata:              `{"seeded_by":"ecommerce_bootstrap"}`,
	})
	return err
}

func (s *Service) resolveProgramID(programCode string) (string, error) {
	if strings.TrimSpace(programCode) == "" {
		return "", nil
	}
	items, err := s.platform.ListReferralPrograms(s.productCode, "")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ProgramCode == strings.TrimSpace(programCode) {
			return item.ID, nil
		}
	}
	return "", nil
}

func decodeStringMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
