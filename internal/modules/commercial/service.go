package commercial

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/platform"
	"ecommerce-service/internal/repository"

	"gorm.io/gorm"
)

type Service struct {
	platform    *platform.Client
	repo        *repository.CommercialRepository
	productCode string
}

type OfferingsResult struct {
	ProductCode   string                  `json:"product_code"`
	Offerings     *platform.OfferingsView `json:"offerings"`
	WalletSummary *platform.WalletSummary `json:"wallet_summary,omitempty"`
}

type CreateOrderInput struct {
	SKUCode     string `json:"sku_code,omitempty"`
	PackageCode string `json:"package_code,omitempty"`
	Quantity    int64  `json:"quantity,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
}

type ConfirmOrderPaymentInput struct {
	PaymentMethod     string `json:"payment_method,omitempty"`
	ProviderCode      string `json:"provider_code,omitempty"`
	PaymentAssetCode  string `json:"payment_asset_code,omitempty"`
	ExternalPaymentID string `json:"external_payment_id,omitempty"`
	Metadata          string `json:"metadata,omitempty"`
}

type OrderView struct {
	Order         *models.CommercialOrder       `json:"order,omitempty"`
	Payment       *models.CommercialPayment     `json:"payment,omitempty"`
	Fulfillment   *models.CommercialFulfillment `json:"fulfillment,omitempty"`
	WalletSummary *platform.WalletSummary       `json:"wallet_summary,omitempty"`
}

type OrdersResult struct {
	Items []OrderView `json:"items"`
}

type orderBundle struct {
	SKU        *platform.SKU
	Package    *platform.CommercialPackage
	UnitAmount int64
	Currency   string
}

const (
	ecomPaymentAssetCode        = "ECOMMERCE_CASH"
	ecomCreditsPaymentAssetCode = "ECOMMERCE_CREDIT"
	ecomCreditsPerRMB           = int64(10)
)

func NewService(platformClient *platform.Client, repo *repository.CommercialRepository, appCfg config.AppConfig) *Service {
	return &Service{platform: platformClient, repo: repo, productCode: defaultString(appCfg.ProductCode, "ecommerce")}
}

func (s *Service) Offerings(orgID string) (*OfferingsResult, error) {
	if s.platform == nil {
		return nil, errors.New("platform client unavailable")
	}
	offerings, err := s.platform.GetCatalogOfferings(s.productCode)
	if err != nil {
		return nil, err
	}
	var summary *platform.WalletSummary
	if strings.TrimSpace(orgID) != "" {
		summary, _ = s.platform.GetWalletSummary("organization", orgID, s.productCode)
	}
	return &OfferingsResult{
		ProductCode:   s.productCode,
		Offerings:     offerings,
		WalletSummary: summary,
	}, nil
}

func (s *Service) CreateOrder(userID, orgID string, input CreateOrderInput) (*OrderView, error) {
	if s.platform == nil {
		return nil, errors.New("platform client unavailable")
	}
	if s.repo == nil {
		return nil, errors.New("commercial repository unavailable")
	}
	offerings, err := s.platform.GetCatalogOfferings(s.productCode)
	if err != nil {
		return nil, err
	}
	orderBundle, err := s.resolveOrderBundle(offerings, strings.TrimSpace(input.SKUCode), strings.TrimSpace(input.PackageCode))
	if err != nil {
		return nil, err
	}
	if orderBundle.Package.PackageType == "subscription" {
		if existing, findErr := s.repo.FindLatestFulfilledSubscriptionOrder(orgID, orderBundle.Package.Code); findErr == nil && existing != nil {
			return nil, fmt.Errorf("active subscription already exists for package %s", orderBundle.Package.Code)
		} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, findErr
		}
	}
	metadata, err := s.buildOrderMetadata(input.Metadata, orderBundle)
	if err != nil {
		return nil, err
	}
	quantity := input.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	now := time.Now().UTC()
	order := &models.CommercialOrder{
		UserID:            userID,
		OrganizationID:    orgID,
		ProductCode:       s.productCode,
		SKUCode:           orderBundle.SKU.Code,
		PackageCode:       orderBundle.Package.Code,
		PackageType:       orderBundle.Package.PackageType,
		Currency:          orderBundle.Currency,
		Quantity:          quantity,
		UnitAmount:        orderBundle.UnitAmount,
		TotalAmount:       orderBundle.UnitAmount * quantity,
		Status:            "pending_payment",
		PaymentStatus:     "pending",
		FulfillmentStatus: "pending",
		MetadataJSON:      metadata,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.repo.CreateOrder(order); err != nil {
		return nil, err
	}
	return &OrderView{Order: order}, nil
}

func (s *Service) ListOrders(orgID string, limit, offset int) (*OrdersResult, error) {
	if s.repo == nil {
		return nil, errors.New("commercial repository unavailable")
	}
	items, err := s.repo.ListOrders(orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]OrderView, 0, len(items))
	for i := range items {
		view, err := s.buildOrderView(&items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return &OrdersResult{Items: out}, nil
}

func (s *Service) GetOrder(orgID, orderID string) (*OrderView, error) {
	if s.repo == nil {
		return nil, errors.New("commercial repository unavailable")
	}
	order, err := s.repo.FindOrderByID(orgID, orderID)
	if err != nil {
		return nil, err
	}
	return s.buildOrderView(order)
}

func (s *Service) ConfirmOrderPayment(userID, orgID, orderID string, input ConfirmOrderPaymentInput) (*OrderView, error) {
	if s.repo == nil || s.platform == nil {
		return nil, errors.New("commercial dependencies unavailable")
	}
	order, err := s.repo.FindOrderByID(orgID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status == "fulfilled" || (order.PaymentStatus == "succeeded" && order.FulfillmentStatus == "succeeded") {
		return s.buildOrderView(order)
	}
	now := time.Now().UTC()
	paymentAssetCode := defaultString(strings.TrimSpace(input.PaymentAssetCode), paymentAssetCodeFromMetadata(decodeMap(order.MetadataJSON)))
	if paymentAssetCode == "" {
		paymentAssetCode = ecomPaymentAssetCode
	}
	var paymentAssetType string
	var paymentCurrency string
	var paymentAmount int64
	payment, _ := s.repo.FindLatestPaymentByOrderID(order.ID)
	if payment == nil || payment.Status != "succeeded" {
		paymentAssetType, paymentCurrency, paymentAmount, err = resolvePaymentCharge(order, paymentAssetCode)
		if err != nil {
			return nil, err
		}
		_, _, walletLedger, err := s.platform.PostWalletLedger(platform.PostWalletLedgerInput{
			BillingSubjectType: "organization",
			BillingSubjectID:   orgID,
			AssetCode:          paymentAssetCode,
			AssetType:          paymentAssetType,
			Direction:          "debit",
			Amount:             paymentAmount,
			Reason:             "commercial_order_payment",
			ReferenceType:      "commercial_order",
			ReferenceID:        order.ID,
			Metadata:           order.MetadataJSON,
		})
		if err != nil {
			return nil, err
		}
		paymentMetadata, err := encodeMap(mergeMaps(
			decodeMap(order.MetadataJSON),
			decodeMap(input.Metadata),
			map[string]any{
				"source":             "ecommerce_commercial_payment_confirm",
				"order_id":           order.ID,
				"payment_asset_code": paymentAssetCode,
				"wallet_ledger_id":   walletLedger.ID,
			},
		))
		if err != nil {
			return nil, err
		}
		payment = &models.CommercialPayment{
			OrderID:           order.ID,
			UserID:            userID,
			OrganizationID:    orgID,
			Amount:            paymentAmount,
			Currency:          paymentCurrency,
			PaymentMethod:     defaultString(strings.TrimSpace(input.PaymentMethod), "wallet_balance"),
			ProviderCode:      defaultString(strings.TrimSpace(input.ProviderCode), "platform_wallet"),
			ExternalPaymentID: defaultString(strings.TrimSpace(input.ExternalPaymentID), walletLedger.ID),
			Status:            "succeeded",
			MetadataJSON:      paymentMetadata,
			PaidAt:            &now,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := s.repo.CreatePayment(payment); err != nil {
			return nil, err
		}
	}
	if order.PaymentStatus != "succeeded" {
		order.PaymentStatus = "succeeded"
		order.Status = "paid"
		order.PaidAt = &now
		order.UpdatedAt = now
		if err := s.repo.SaveOrder(order); err != nil {
			return nil, err
		}
	}
	fulfillment, _ := s.repo.FindLatestFulfillmentByOrderID(order.ID)
	var assignResult *packageAssignResult
	if fulfillment == nil || fulfillment.Status != "succeeded" {
		assignResult, err = s.assignPackage(userID, orgID, order, payment, input.Metadata)
		if err != nil {
			order.Status = "payment_succeeded_fulfillment_failed"
			order.FulfillmentStatus = "failed"
			order.UpdatedAt = time.Now().UTC()
			_ = s.repo.SaveOrder(order)
			return nil, err
		}
		fulfillment = &models.CommercialFulfillment{
			OrderID:           order.ID,
			UserID:            userID,
			OrganizationID:    orgID,
			PackageCode:       order.PackageCode,
			FulfillmentMode:   assignResult.FulfillmentMode,
			Status:            "succeeded",
			AssetCode:         assignResult.AssetCode,
			Amount:            assignResult.Amount,
			AllowancePolicyID: assignResult.AllowancePolicyID,
			CycleKey:          assignResult.CycleKey,
			MetadataJSON:      assignResult.Metadata,
			ExpiresAt:         assignResult.ExpiresAt,
			FulfilledAt:       &now,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if assignResult.WalletAccount != nil {
			fulfillment.WalletAccountID = assignResult.WalletAccount.ID
		}
		if assignResult.WalletBucket != nil {
			fulfillment.WalletBucketID = assignResult.WalletBucket.ID
		}
		if assignResult.WalletLedger != nil {
			fulfillment.WalletLedgerID = assignResult.WalletLedger.ID
		}
		if err := s.repo.CreateFulfillment(fulfillment); err != nil {
			return nil, err
		}
	}
	order.Status = "fulfilled"
	order.FulfillmentStatus = "succeeded"
	order.FulfilledAt = &now
	order.UpdatedAt = now
	if err := s.repo.SaveOrder(order); err != nil {
		return nil, err
	}
	walletSummary := (*platform.WalletSummary)(nil)
	if assignResult != nil {
		walletSummary = assignResult.WalletSummary
	}
	if walletSummary == nil {
		walletSummary, _ = s.platform.GetWalletSummary("organization", orgID, order.ProductCode)
	}
	return &OrderView{
		Order:         order,
		Payment:       payment,
		Fulfillment:   fulfillment,
		WalletSummary: walletSummary,
	}, nil
}

type packageAssignResult struct {
	FulfillmentMode   string
	AssetCode         string
	Amount            int64
	GrantedQuotaUnits int64
	AllowancePolicyID string
	CycleKey          string
	Metadata          string
	ExpiresAt         *time.Time
	WalletAccount     *platform.WalletAccount
	WalletBucket      *platform.WalletBucket
	WalletLedger      *platform.WalletLedger
	WalletSummary     *platform.WalletSummary
}

func (s *Service) assignPackage(_ string, orgID string, order *models.CommercialOrder, payment *models.CommercialPayment, inputMetadata string) (*packageAssignResult, error) {
	offerings, err := s.platform.GetCatalogOfferings(order.ProductCode)
	if err != nil {
		return nil, err
	}
	pkg := findCommercialPackage(offerings.Packages, order.PackageCode)
	if pkg == nil {
		return nil, fmt.Errorf("package not found: %s", order.PackageCode)
	}
	metadata, err := encodeMap(mergeMaps(
		decodeMap(order.MetadataJSON),
		decodeMap(inputMetadata),
		map[string]any{
			"source":             "ecommerce_commercial_order_fulfillment",
			"order_id":           order.ID,
			"payment_id":         payment.ID,
			"sku_code":           order.SKUCode,
			"payment_asset_code": paymentAssetCodeFromMetadata(decodeMap(payment.MetadataJSON)),
		},
	))
	if err != nil {
		return nil, err
	}
	quotaPolicies, err := s.platform.ListQuotaGrantPolicies(order.ProductCode, pkg.Code)
	if err != nil {
		return nil, err
	}
	if len(quotaPolicies) == 0 {
		return nil, fmt.Errorf("quota grant policy not found for package: %s", pkg.Code)
	}
	var grantedUnits int64
	for _, policy := range quotaPolicies {
		if policy.Status != "active" || policy.Units <= 0 {
			continue
		}
		if err := s.platform.GrantQuota(platform.GrantQuotaInput{
			BillingSubjectType: "organization",
			BillingSubjectID:   orgID,
			BillableItemCode:   policy.BillableItemCode,
			Units:              policy.Units,
			Reason:             "commercial_package_purchase",
			ReferenceID:        order.ID,
		}); err != nil {
			return nil, err
		}
		grantedUnits += policy.Units
	}
	summary, _ := s.platform.GetWalletSummary("organization", orgID, order.ProductCode)
	return &packageAssignResult{
		FulfillmentMode:   "quota_grant",
		Amount:            grantedUnits,
		GrantedQuotaUnits: grantedUnits,
		Metadata:          metadata,
		WalletSummary:     summary,
	}, nil
}

func (s *Service) resolveOrderBundle(offerings *platform.OfferingsView, skuCode, packageCode string) (*orderBundle, error) {
	if offerings == nil {
		return nil, errors.New("offerings unavailable")
	}
	var matchedPackage *platform.CommercialPackage
	if packageCode != "" {
		matchedPackage = findCommercialPackage(offerings.Packages, packageCode)
		if matchedPackage == nil {
			return nil, fmt.Errorf("package not found: %s", packageCode)
		}
	}
	var matchedSKU *platform.SKU
	if skuCode != "" {
		matchedSKU = findSKU(offerings.SKUs, skuCode)
		if matchedSKU == nil {
			return nil, fmt.Errorf("sku not found: %s", skuCode)
		}
	}
	if matchedPackage == nil && matchedSKU == nil {
		return nil, errors.New("sku_code or package_code is required")
	}
	if matchedPackage == nil && matchedSKU != nil {
		skuMetadata := decodeMap(matchedSKU.Metadata)
		pkgCode, _ := skuMetadata["package_code"].(string)
		matchedPackage = findCommercialPackage(offerings.Packages, pkgCode)
		if matchedPackage == nil {
			return nil, fmt.Errorf("package not found for sku: %s", matchedSKU.Code)
		}
	}
	if matchedSKU == nil && matchedPackage != nil {
		pkgMetadata := decodeMap(matchedPackage.Metadata)
		skuCodeFromPkg, _ := pkgMetadata["sku_code"].(string)
		matchedSKU = findSKU(offerings.SKUs, skuCodeFromPkg)
		if matchedSKU == nil {
			return nil, fmt.Errorf("sku not found for package: %s", matchedPackage.Code)
		}
	}
	if matchedPackage.Status != "active" || matchedSKU.Status != "active" {
		return nil, errors.New("commercial sku/package is not active")
	}
	rateCard := findBestRateCardForOrder(offerings.RateCards, matchedSKU, matchedPackage.Code)
	currency := matchedSKU.Currency
	unitAmount := matchedSKU.ListPrice
	if rateCard != nil {
		rateConfig := decodeMap(rateCard.PriceConfig)
		if amount := int64MapValue(rateConfig, "unit_amount"); amount > 0 {
			unitAmount = amount
		}
		if strings.TrimSpace(rateCard.Currency) != "" {
			currency = rateCard.Currency
		}
	}
	return &orderBundle{
		SKU:        matchedSKU,
		Package:    matchedPackage,
		UnitAmount: unitAmount,
		Currency:   currency,
	}, nil
}

func (s *Service) buildOrderMetadata(raw string, bundle *orderBundle) (string, error) {
	inputMetadata, err := decodeMapStrict(raw)
	if err != nil {
		return "", err
	}
	return encodeMap(mergeMaps(inputMetadata, map[string]any{
		"sku_code":           bundle.SKU.Code,
		"package_code":       bundle.Package.Code,
		"package_type":       bundle.Package.PackageType,
		"product_code":       s.productCode,
		"payment_asset_code": ecomPaymentAssetCode,
	}))
}

func (s *Service) buildOrderView(order *models.CommercialOrder) (*OrderView, error) {
	view := &OrderView{Order: order}
	if order == nil || s.repo == nil {
		return view, nil
	}
	if payment, err := s.repo.FindLatestPaymentByOrderID(order.ID); err == nil {
		view.Payment = payment
	}
	if fulfillment, err := s.repo.FindLatestFulfillmentByOrderID(order.ID); err == nil {
		view.Fulfillment = fulfillment
	}
	if s.platform != nil {
		summary, _ := s.platform.GetWalletSummary("organization", order.OrganizationID, order.ProductCode)
		view.WalletSummary = summary
	}
	return view, nil
}

func encodeMap(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func decodeMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func decodeMapStrict(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid metadata: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func mergeMaps(items ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		for key, value := range item {
			out[key] = value
		}
	}
	return out
}

func findCommercialPackage(items []platform.CommercialPackage, packageCode string) *platform.CommercialPackage {
	for i := range items {
		if items[i].Code == packageCode {
			return &items[i]
		}
	}
	return nil
}

func findSKU(items []platform.SKU, skuCode string) *platform.SKU {
	for i := range items {
		if items[i].Code == skuCode {
			return &items[i]
		}
	}
	return nil
}

func findAssetDefinition(items []platform.AssetDefinition, assetCode string) *platform.AssetDefinition {
	for i := range items {
		if items[i].AssetCode == assetCode {
			return &items[i]
		}
	}
	return nil
}

func computeAssetExpiry(now time.Time, item *platform.AssetDefinition) *time.Time {
	if item == nil || item.DefaultExpireDays <= 0 {
		return nil
	}
	expireAt := now.AddDate(0, 0, item.DefaultExpireDays)
	return &expireAt
}

func findBestRateCardForOrder(items []platform.RateCard, sku *platform.SKU, packageCode string) *platform.RateCard {
	var best *platform.RateCard
	for i := range items {
		item := &items[i]
		if item.Status != "active" {
			continue
		}
		matchBySKU := item.TargetType == "sku" && item.TargetID == sku.ID
		matchByPackage := stringMapValue(decodeMap(item.Metadata), "package_code") == packageCode
		if !matchBySKU && !matchByPackage {
			continue
		}
		if best == nil || item.Version > best.Version {
			best = item
		}
	}
	return best
}

func paymentAssetCodeFromMetadata(values map[string]any) string {
	if values == nil {
		return ""
	}
	return stringMapValue(values, "payment_asset_code")
}

func resolvePaymentCharge(order *models.CommercialOrder, paymentAssetCode string) (assetType string, currency string, amount int64, err error) {
	switch paymentAssetCode {
	case "", ecomPaymentAssetCode:
		return "cash_balance", defaultString(order.Currency, "CNY"), order.TotalAmount, nil
	case ecomCreditsPaymentAssetCode:
		return "wallet_credit", ecomCreditsPaymentAssetCode, convertCashAmountToCredits(order.TotalAmount), nil
	case "ECOMMERCE_PROMO_CREDIT":
		return "reward_credit", "ECOMMERCE_PROMO_CREDIT", convertCashAmountToCredits(order.TotalAmount), nil
	default:
		return "", "", 0, fmt.Errorf("unsupported payment asset code: %s", paymentAssetCode)
	}
}

func convertCashAmountToCredits(cents int64) int64 {
	if cents <= 0 {
		return 0
	}
	return (cents*ecomCreditsPerRMB + 99) / 100
}

func int64MapValue(values map[string]any, key string) int64 {
	if values == nil {
		return 0
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case json.Number:
		i, err := typed.Int64()
		if err == nil {
			return i
		}
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return 0
		}
		var n json.Number = json.Number(typed)
		if i, err := n.Int64(); err == nil {
			return i
		}
	}
	return 0
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(typed)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
