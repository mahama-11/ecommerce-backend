package commission

import (
	"sort"
	"strings"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/platform"
)

type Service struct {
	platform    *platform.Client
	productCode string
	redeemAsset string
}

type Overview struct {
	Commissions           []platform.CommissionLedger `json:"commissions"`
	TotalCommission       int64                       `json:"total_commission"`
	EarnedCommission      int64                       `json:"earned_commission"`
	PendingCommission     int64                       `json:"pending_commission"`
	ReversedCommission    int64                       `json:"reversed_commission"`
	RedeemedCommission    int64                       `json:"redeemed_commission"`
	RedeemableCommission  int64                       `json:"redeemable_commission"`
	RedeemTargetAssetCode string                      `json:"redeem_target_asset_code"`
}

type ChannelPartnerSummary struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	PartnerType string `json:"partner_type"`
	Status      string `json:"status"`
	RiskLevel   string `json:"risk_level"`
}

type ChannelProgramSummary struct {
	ID          string `json:"id"`
	ProgramCode string `json:"program_code"`
	Name        string `json:"name"`
	ProgramType string `json:"program_type"`
	Status      string `json:"status"`
}

type ChannelBindingView struct {
	Binding platform.ChannelBinding `json:"binding"`
	Partner *ChannelPartnerSummary  `json:"partner,omitempty"`
	Program *ChannelProgramSummary  `json:"program,omitempty"`
}

type ChannelCommissionView struct {
	Partner *ChannelPartnerSummary           `json:"partner,omitempty"`
	Program *ChannelProgramSummary           `json:"program,omitempty"`
	Ledger  platform.ChannelCommissionLedger `json:"ledger"`
}

type ChannelSettlementView struct {
	Partner *ChannelPartnerSummary           `json:"partner,omitempty"`
	Program *ChannelProgramSummary           `json:"program,omitempty"`
	Batch   *platform.ChannelSettlementBatch `json:"batch,omitempty"`
	Item    platform.ChannelSettlementItem   `json:"item"`
}

type ChannelOverview struct {
	Partners           []ChannelPartnerSummary `json:"partners"`
	CurrentBindings    []ChannelBindingView    `json:"current_bindings"`
	TotalCommission    int64                   `json:"total_commission"`
	PendingCommission  int64                   `json:"pending_commission"`
	EarnedCommission   int64                   `json:"earned_commission"`
	SettledCommission  int64                   `json:"settled_commission"`
	ReversedCommission int64                   `json:"reversed_commission"`
	SettlementCount    int                     `json:"settlement_count"`
	RecentSettlements  []ChannelSettlementView `json:"recent_settlements"`
}

type RedeemInput struct {
	CommissionIDs []string `json:"commission_ids"`
	Metadata      string   `json:"metadata"`
}

func NewService(platformClient *platform.Client, appCfg config.AppConfig) *Service {
	return &Service{
		platform:    platformClient,
		productCode: defaultString(appCfg.ProductCode, "ecommerce"),
		redeemAsset: defaultString(appCfg.RewardAssetCode, "ECOMMERCE_PROMO_CREDIT"),
	}
}

func (s *Service) ListCommissions(orgID, status string) ([]platform.CommissionLedger, error) {
	items, err := s.platform.ListCommissions(s.productCode, "organization", orgID, status)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) Overview(orgID, status string) (*Overview, error) {
	items, err := s.ListCommissions(orgID, status)
	if err != nil {
		return nil, err
	}
	out := &Overview{Commissions: items, RedeemTargetAssetCode: s.redeemAsset}
	for _, item := range items {
		out.TotalCommission += item.Amount
		switch item.Status {
		case "earned":
			out.EarnedCommission += item.Amount
			out.RedeemableCommission += item.Amount
		case "redeemed":
			out.RedeemedCommission += item.Amount
		case "reversed":
			out.ReversedCommission += item.Amount
		default:
			out.PendingCommission += item.Amount
		}
	}
	return out, nil
}

func (s *Service) Redeem(orgID string, input RedeemInput) (*platform.RedeemCommissionsResult, error) {
	return s.platform.RedeemCommissions(platform.RedeemCommissionsInput{
		ProductCode:            s.productCode,
		BeneficiarySubjectType: "organization",
		BeneficiarySubjectID:   orgID,
		AssetCode:              s.redeemAsset,
		CommissionIDs:          input.CommissionIDs,
		Metadata:               input.Metadata,
	})
}

func (s *Service) CurrentBindings(orgID string) ([]ChannelBindingView, error) {
	bindings, err := s.platform.ListChannelBindings(s.productCode, orgID, "")
	if err != nil {
		return nil, err
	}
	partners, programs, err := s.loadPartnerProgramMaps()
	if err != nil {
		return nil, err
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].CreatedAt.After(bindings[j].CreatedAt) })
	out := make([]ChannelBindingView, 0, len(bindings))
	for _, item := range bindings {
		out = append(out, ChannelBindingView{
			Binding: item,
			Partner: partners[item.ChannelPartnerID],
			Program: programs[item.ChannelProgramID],
		})
	}
	return out, nil
}

func (s *Service) ChannelOverview(orgID string) (*ChannelOverview, error) {
	bindings, err := s.CurrentBindings(orgID)
	if err != nil {
		return nil, err
	}
	partners, err := s.resolvePartnersForOrg(orgID)
	if err != nil {
		return nil, err
	}
	out := &ChannelOverview{CurrentBindings: bindings}
	if len(partners) == 0 {
		return out, nil
	}
	partnerViews := make([]ChannelPartnerSummary, 0, len(partners))
	for _, item := range partners {
		partnerViews = append(partnerViews, mapPartner(item))
	}
	sort.Slice(partnerViews, func(i, j int) bool { return partnerViews[i].Code < partnerViews[j].Code })
	out.Partners = partnerViews
	commissions, err := s.ListChannelCommissions(orgID, "")
	if err != nil {
		return nil, err
	}
	for _, item := range commissions {
		out.TotalCommission += item.Ledger.CommissionAmount
		switch item.Ledger.Status {
		case "pending":
			out.PendingCommission += item.Ledger.CommissionAmount
		case "earned", "settlement_in_progress":
			out.EarnedCommission += item.Ledger.CommissionAmount
		case "settled":
			out.SettledCommission += item.Ledger.CommissionAmount
		case "reversed", "void":
			out.ReversedCommission += item.Ledger.CommissionAmount
		}
	}
	settlements, err := s.ListChannelSettlements(orgID, "")
	if err != nil {
		return nil, err
	}
	out.SettlementCount = len(settlements)
	if len(settlements) > 5 {
		out.RecentSettlements = settlements[:5]
	} else {
		out.RecentSettlements = settlements
	}
	return out, nil
}

func (s *Service) ListChannelCommissions(orgID, status string) ([]ChannelCommissionView, error) {
	partners, err := s.resolvePartnersForOrg(orgID)
	if err != nil {
		return nil, err
	}
	if len(partners) == 0 {
		return []ChannelCommissionView{}, nil
	}
	programs, err := s.listProgramsMap()
	if err != nil {
		return nil, err
	}
	out := make([]ChannelCommissionView, 0)
	for _, partner := range partners {
		items, err := s.platform.ListChannelCommissions(s.productCode, partner.ID, status)
		if err != nil {
			return nil, err
		}
		partnerSummary := mapPartner(partner)
		for _, item := range items {
			out = append(out, ChannelCommissionView{
				Partner: &partnerSummary,
				Program: programs[item.ChannelProgramID],
				Ledger:  item,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ledger.CreatedAt.After(out[j].Ledger.CreatedAt) })
	return out, nil
}

func (s *Service) ListChannelSettlements(orgID, status string) ([]ChannelSettlementView, error) {
	partners, err := s.resolvePartnersForOrg(orgID)
	if err != nil {
		return nil, err
	}
	if len(partners) == 0 {
		return []ChannelSettlementView{}, nil
	}
	programs, err := s.listProgramsMap()
	if err != nil {
		return nil, err
	}
	out := make([]ChannelSettlementView, 0)
	for _, partner := range partners {
		partnerSummary := mapPartner(partner)
		for _, program := range programs {
			batches, err := s.platform.ListChannelSettlementBatches(s.productCode, program.ID, status)
			if err != nil {
				return nil, err
			}
			for _, batch := range batches {
				detail, err := s.platform.GetChannelSettlementBatch(batch.ID)
				if err != nil {
					return nil, err
				}
				for _, item := range detail.Items {
					if item.Item.ChannelPartnerID != partner.ID {
						continue
					}
					programCopy := *program
					batchCopy := detail.Batch
					out = append(out, ChannelSettlementView{
						Partner: &partnerSummary,
						Program: &programCopy,
						Batch:   &batchCopy,
						Item:    item.Item,
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Item.CreatedAt.After(out[j].Item.CreatedAt)
	})
	return out, nil
}

func (s *Service) loadPartnerProgramMaps() (map[string]*ChannelPartnerSummary, map[string]*ChannelProgramSummary, error) {
	partners, err := s.platform.ListChannelPartners("active")
	if err != nil {
		return nil, nil, err
	}
	programs, err := s.platform.ListChannelPrograms(s.productCode, "active")
	if err != nil {
		return nil, nil, err
	}
	partnerMap := make(map[string]*ChannelPartnerSummary, len(partners))
	for _, item := range partners {
		summary := mapPartner(item)
		partnerMap[item.ID] = &summary
	}
	programMap := make(map[string]*ChannelProgramSummary, len(programs))
	for _, item := range programs {
		summary := mapProgram(item)
		programMap[item.ID] = &summary
	}
	return partnerMap, programMap, nil
}

func (s *Service) listProgramsMap() (map[string]*ChannelProgramSummary, error) {
	_, programs, err := s.loadPartnerProgramMaps()
	return programs, err
}

func (s *Service) resolvePartnersForOrg(orgID string) ([]platform.ChannelPartner, error) {
	bindings, err := s.platform.ListChannelBindings(s.productCode, orgID, "")
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return []platform.ChannelPartner{}, nil
	}
	partners, err := s.platform.ListChannelPartners("active")
	if err != nil {
		return nil, err
	}
	needed := map[string]struct{}{}
	for _, item := range bindings {
		needed[item.ChannelPartnerID] = struct{}{}
	}
	out := make([]platform.ChannelPartner, 0, len(needed))
	for _, item := range partners {
		if _, ok := needed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func mapPartner(item platform.ChannelPartner) ChannelPartnerSummary {
	return ChannelPartnerSummary{
		ID:          item.ID,
		Code:        item.Code,
		Name:        item.Name,
		PartnerType: item.PartnerType,
		Status:      item.Status,
		RiskLevel:   item.RiskLevel,
	}
}

func mapProgram(item platform.ChannelProgram) ChannelProgramSummary {
	return ChannelProgramSummary{
		ID:          item.ID,
		ProgramCode: item.ProgramCode,
		Name:        item.Name,
		ProgramType: item.ProgramType,
		Status:      item.Status,
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
