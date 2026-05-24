package productcore

import (
	"ecommerce-service/internal/billinggate"
	"ecommerce-service/internal/models"
	"ecommerce-service/internal/repository"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ListProductAssets 列出商品关联的资产
func (s *Service) ListProductAssets(orgID string, productID string) ([]ProductAssetWithDetail, error) {
	scope := repository.Scope{OrgID: orgID}
	relations, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return nil, err
	}

	assetsWithDetail := make([]ProductAssetWithDetail, 0, len(relations))
	for _, rel := range relations {
		var asset *models.EcommerceAsset
		asset, _ = s.assetRepo.FindAssetByIDGlobal(rel.AssetID)
		assetsWithDetail = append(assetsWithDetail, ProductAssetWithDetail{
			Relation: rel,
			Asset:    asset,
		})
	}
	return assetsWithDetail, nil
}

// AddProductAsset 添加商品资产关联
func (s *Service) AddProductAsset(orgID string, userID string, productID string, input AddProductAssetInput) (*models.EcomAssetRelation, error) {

	if input.AssetID == "" {
		return nil, fmt.Errorf("asset ID is required")
	}
	if !isValidAssetRelationType(input.RelationType) {
		return nil, fmt.Errorf("invalid asset relation type")
	}
	if input.AssetRole == "" {
		return nil, fmt.Errorf("asset role is required")
	}
	if !isValidAssetRole(input.AssetRole) {
		return nil, fmt.Errorf("invalid asset role")
	}

	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}
	if _, err := s.assetRepo.FindAssetByID(orgID, input.AssetID); err != nil {
		return nil, fmt.Errorf("asset not found")
	}
	if _, err := s.repo.FindProductAssetRelation(scope, productID, input.AssetID); err == nil {
		return nil, fmt.Errorf("asset is already linked to product")
	}
	if input.IsPrimary {
		if err := s.repo.ClearPrimaryProductAssets(scope, productID, ""); err != nil {
			return nil, err
		}
	}
	item := models.EcomAssetRelation{
		ID:           uuid.New().String(),
		AssetID:      input.AssetID,
		OwnerType:    models.AssetRelationOwnerTypeProduct,
		OwnerID:      productID,
		RelationType: input.RelationType,
		AssetRole:    input.AssetRole,
		IsPrimary:    input.IsPrimary,
		PlatformCode: input.PlatformCode,
		SiteCode:     input.SiteCode,
		LocaleCode:   input.LocaleCode,
		SortOrder:    input.SortOrder,
	}
	relation, err := s.repo.AddProductAsset(scope, item)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeAssetCreated,
		"Asset Added", "Asset role: "+input.AssetRole, nil)

	s.updateProductAssetStatus(scope, productID)

	s.updateProductMainStatus(scope, productID)

	return relation, nil
}

// UpdateProductAsset 更新商品资产关联
func (s *Service) UpdateProductAsset(orgID string, userID string, productID string, assetRelationID string, input UpdateProductAssetInput) (*models.EcomAssetRelation, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if _, err := s.repo.GetProduct(scope, productID); err != nil {
		return nil, err
	}
	relation, err := s.repo.GetProductAssetRelation(scope, productID, assetRelationID)
	if err != nil {
		return nil, err
	}
	if input.RelationType != nil {
		if strings.TrimSpace(*input.RelationType) == "" || !isValidAssetRelationType(*input.RelationType) {
			return nil, fmt.Errorf("invalid asset relation type")
		}
		relation.RelationType = strings.TrimSpace(*input.RelationType)
	}
	if input.AssetRole != nil {
		if strings.TrimSpace(*input.AssetRole) == "" || !isValidAssetRole(*input.AssetRole) {
			return nil, fmt.Errorf("invalid asset role")
		}
		relation.AssetRole = strings.TrimSpace(*input.AssetRole)
	}
	if input.PlatformCode != nil {
		relation.PlatformCode = strings.TrimSpace(*input.PlatformCode)
	}
	if input.SiteCode != nil {
		relation.SiteCode = strings.TrimSpace(*input.SiteCode)
	}
	if input.LocaleCode != nil {
		relation.LocaleCode = strings.TrimSpace(*input.LocaleCode)
	}
	if input.SortOrder != nil {
		relation.SortOrder = *input.SortOrder
	}
	if input.IsPrimary != nil {
		relation.IsPrimary = *input.IsPrimary
		if *input.IsPrimary {
			if clearErr := s.repo.ClearPrimaryProductAssets(scope, productID, relation.ID); clearErr != nil {
				return nil, clearErr
			}
		}
	}
	updated, err := s.repo.UpdateProductAssetRelation(scope, *relation)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeAssetCreated,
		"Asset Relation Updated", "Asset relation metadata updated for role "+updated.AssetRole, nil)
	s.updateProductAssetStatus(scope, productID)
	s.updateProductMainStatus(scope, productID)
	return updated, nil
}

// DeleteProductAsset 删除商品资产关联
func (s *Service) DeleteProductAsset(orgID string, userID string, productID string, assetRelationID string) error {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	if err := s.repo.DeleteProductAsset(scope, assetRelationID); err != nil {
		return err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeAssetDeleted,
		"Asset Deleted", "Asset relation ID: "+assetRelationID, nil)

	s.updateProductAssetStatus(scope, productID)

	s.updateProductMainStatus(scope, productID)

	return nil
}

// updateProductAssetStatus 更新商品的资产状态
func (s *Service) updateProductAssetStatus(scope repository.Scope, productID string) {
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return
	}

	assets, err := s.repo.ListProductAssets(scope, productID)
	if err != nil {
		return
	}

	var newStatus string
	if len(assets) == 0 {
		newStatus = models.AssetStatusMissing
	} else {

		hasHero := false
		hasModel := false
		for _, a := range assets {
			if a.AssetRole == models.AssetRoleHero {
				hasHero = true
			}
			if a.AssetRole == models.AssetRoleModelShot {
				hasModel = true
			}
		}
		if hasHero && hasModel {
			newStatus = models.AssetStatusReady
		} else {
			newStatus = models.AssetStatusPartial
		}
	}
	product.AssetStatus = newStatus
	s.repo.UpdateProduct(scope, *product)
}

// ListListingVersions 列出商品的 Listing 版本
func (s *Service) ListListingVersions(orgID string, productID string) ([]models.EcomListingVersion, error) {
	return s.repo.ListListingVersions(repository.Scope{OrgID: orgID}, productID)
}

// CreateListingVersion 创建 Listing 版本
func (s *Service) CreateListingVersion(orgID string, userID string, productID string, input CreateListingVersionInput) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	product, err := s.repo.GetProduct(scope, strings.TrimSpace(productID))
	if err != nil {
		return nil, err
	}
	return s.createListingVersionForProduct(scope, product, input, false)
}

func (s *Service) createListingVersionForProduct(scope repository.Scope, product *models.EcomProductSKU, input CreateListingVersionInput, preview bool) (*models.EcomListingVersion, error) {
	if product == nil {
		return nil, fmt.Errorf("product not found")
	}
	input = normalizeCreateListingInput(input)
	assetReady := product.AssetStatus == models.AssetStatusReady
	if !assetReady {
		if relations, relErr := s.repo.ListProductAssets(scope, product.ID); relErr == nil && len(relations) > 0 {
			assetReady = true
		}
	}
	if err := validateListingInput(product, input, assetReady); err != nil {
		return nil, err
	}
	versionNo, _ := s.repo.GetNextListingVersionNo(scope, product.ID)
	versionID := uuid.New().String()
	item := models.EcomListingVersion{
		ID:           versionID,
		ProductID:    product.ID,
		VersionNo:    versionNo,
		VersionLabel: input.VersionLabel,
		Status:       models.ListingVersionStatusDraft,
		Title:        input.Title,
		Description:  input.Description,
		BulletPoints: pq.StringArray(input.BulletPoints),
		Keywords:     pq.StringArray(input.Keywords),
		Platform:     input.Platform,
		Site:         input.Site,
		Locale:       input.Locale,
	}
	if preview {
		item.OrganizationID = scope.OrgID
		item.CreatedBy = scope.UserID
		return &item, nil
	}

	chargeCtx, err := s.prepareListingChargeContext(scope, product, item)
	if err != nil {
		return nil, err
	}
	listing, err := s.repo.CreateListingVersion(scope, item)
	if err != nil {
		_ = billinggate.New(s.platform).Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_create_failed"})
		return nil, err
	}
	gate := billinggate.New(s.platform)
	if err := gate.MarkReserved(chargeCtx, map[string]any{"listing_version_id": listing.ID, "product_id": product.ID}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_mark_reserved_failed"})
		return nil, err
	}
	if _, err := gate.Commit(billinggate.CommitInput{
		Context:      chargeCtx,
		SourceAction: "listing_version_created",
		EventID:      fmt.Sprintf("evt_%s", listing.ID),
		Dimensions: map[string]any{"platform": listing.Platform, "site": listing.Site, "locale": listing.Locale,
			"sku_code": product.SKUCode},
		Metadata: map[string]any{"listing_version_id": listing.ID, "product_id": product.ID},
	}); err != nil {
		_ = gate.Release(billinggate.ReleaseInput{Context: chargeCtx, Reason: "listing_commit_failed"})
		return nil, err
	}

	s.logActivity(scope, product.ID, models.ProductActivityTypeListingGenerated,
		"Listing Created", "Listing version "+input.VersionLabel+" created for "+input.Platform, nil)

	s.updateProductListingStatus(scope, product.ID)
	s.updateProductMainStatus(scope, product.ID)

	return listing, nil
}

func normalizeCreateListingInput(input CreateListingVersionInput) CreateListingVersionInput {
	input.VersionLabel = strings.TrimSpace(input.VersionLabel)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Site = strings.TrimSpace(input.Site)
	input.Locale = strings.TrimSpace(input.Locale)
	input.BulletPoints = sanitizeStringList(input.BulletPoints)
	input.Keywords = sanitizeStringList(input.Keywords)
	return input
}

func validateListingInput(product *models.EcomProductSKU, input CreateListingVersionInput, assetReady bool) error {
	if strings.TrimSpace(input.VersionLabel) == "" {
		return fmt.Errorf("version label is required")
	}
	if input.Title == "" {
		return fmt.Errorf("title is required")
	}
	if runeLen(input.Title) > listingTitleMaxRunes {
		return fmt.Errorf("title length must be <= %d characters", listingTitleMaxRunes)
	}
	if runeLen(input.Description) > listingDescriptionMaxRunes {
		return fmt.Errorf("description length must be <= %d characters", listingDescriptionMaxRunes)
	}
	if len(input.BulletPoints) > listingBulletMaxCount {
		return fmt.Errorf("bullet point count must be <= %d", listingBulletMaxCount)
	}
	for i, bullet := range input.BulletPoints {
		if runeLen(bullet) > listingBulletMaxRunes {
			return fmt.Errorf("bullet point %d length must be <= %d characters", i+1, listingBulletMaxRunes)
		}
	}
	if input.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	if input.Site == "" {
		return fmt.Errorf("site is required")
	}
	if input.Locale == "" {
		return fmt.Errorf("locale is required")
	}
	if product == nil || strings.TrimSpace(product.SKUCode) == "" {
		return fmt.Errorf("sku_code is required")
	}
	if !assetReady {
		return fmt.Errorf("assets are not ready for sku %s", product.SKUCode)
	}
	allText := []string{input.Title, input.Description}
	allText = append(allText, input.BulletPoints...)
	allText = append(allText, input.Keywords...)
	if word := containsBaselineSensitiveWord(allText...); word != "" {
		return fmt.Errorf("sensitive word detected: %s", word)
	}
	return nil
}

func (s *Service) prepareListingChargeContext(scope repository.Scope, product *models.EcomProductSKU, item models.EcomListingVersion) (*billinggate.Context, error) {
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing platform client is required for listing generation")
	}
	return billinggate.New(s.platform).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionListing,
		SourceType:       billinggate.SourceTypeListingVersion,
		SourceID:         item.ID,
		ProductCode:      billinggate.DefaultProductCode,
		OrganizationID:   scope.OrgID,
		UserID:           scope.UserID,
		BillableItemCode: billinggate.BillableItemListingGenerate,
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   billinggate.IdempotencyKeyForAction(billinggate.ActionListing, item.ID),
		RouteSnapshot:    map[string]any{"platform": item.Platform, "site": item.Site, "locale": item.Locale},
		Metadata:         map[string]any{"product_id": product.ID, "sku_code": product.SKUCode, "version_no": item.VersionNo},
	})
}

func (s *Service) prepareExportChargeContext(scope repository.Scope, productID string, exportID string, input CreateExportTaskInput) (*billinggate.Context, error) {
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("billing platform client is required for export generation")
	}
	return billinggate.New(s.platform).Begin(billinggate.BeginInput{
		Action:           billinggate.ActionExport,
		SourceType:       billinggate.SourceTypeExportTask,
		SourceID:         exportID,
		ProductCode:      billinggate.DefaultProductCode,
		OrganizationID:   scope.OrgID,
		UserID:           scope.UserID,
		BillableItemCode: billinggate.BillableItemExportGenerate,
		ResourceType:     billinggate.DefaultResourceType,
		UsageUnits:       1,
		IdempotencyKey:   billinggate.IdempotencyKeyForAction(billinggate.ActionExport, exportID),
		RouteSnapshot:    map[string]any{"platform": input.Platform, "site": input.Site, "locale": input.Locale, "format": input.Format},
		Metadata:         map[string]any{"product_id": productID},
	})
}

func (s *Service) BatchCreateListingVersions(orgID string, userID string, input BatchCreateListingVersionsInput) (*BatchListingMutationResult, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least 1 listing item is required")
	}

	result := &BatchListingMutationResult{
		Total:   len(input.Items),
		Preview: input.Preview,
		Items:   make([]BatchListingMutationItem, 0, len(input.Items)),
	}
	seenProductIDs := make(map[string]struct{}, len(input.Items))

	for _, entry := range input.Items {
		entry.ProductID = strings.TrimSpace(entry.ProductID)
		entry.SKUCode = strings.TrimSpace(entry.SKUCode)
		item := BatchListingMutationItem{
			ProductID:    entry.ProductID,
			SKUCode:      entry.SKUCode,
			VersionLabel: strings.TrimSpace(entry.VersionLabel),
			Preview:      input.Preview,
		}

		product, err := s.productForBatchListingEntry(scope, entry)
		if err == nil && product != nil {
			item.ProductID = product.ID
			item.SKUCode = product.SKUCode
			item.ProductTitle = product.Title
		}

		switch {
		case entry.ProductID == "" && entry.SKUCode == "":
			item.Message = "product_id or sku_code is required"
		case entry.ProductID != "" && entry.SKUCode != "" && product != nil && !strings.EqualFold(product.SKUCode, entry.SKUCode):
			item.Message = "sku_code does not match product_id"
		case err != nil:
			item.Message = "product not found"
		default:
			if _, exists := seenProductIDs[product.ID]; exists {
				item.Message = "duplicate product in batch request"
			} else {
				seenProductIDs[product.ID] = struct{}{}
				listing, createErr := s.createListingVersionForProduct(scope, product, CreateListingVersionInput{
					VersionLabel: entry.VersionLabel,
					Title:        entry.Title,
					Description:  entry.Description,
					BulletPoints: entry.BulletPoints,
					Keywords:     entry.Keywords,
					Platform:     entry.Platform,
					Site:         entry.Site,
					Locale:       entry.Locale,
				}, input.Preview)
				if createErr != nil {
					item.Message = createErr.Error()
				} else {
					item.Success = true
					if input.Preview {
						item.Message = "listing version preview valid"
					} else {
						item.Message = "listing version created"
					}
					item.VersionID = listing.ID
					item.VersionLabel = listing.VersionLabel
					item.Listing = listing
					result.Succeeded++
				}
			}
		}

		if !item.Success {
			result.Failed++
		}
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func (s *Service) productForBatchListingEntry(scope repository.Scope, entry BatchCreateListingVersionItemInput) (*models.EcomProductSKU, error) {
	if strings.TrimSpace(entry.ProductID) != "" {
		return s.repo.GetProduct(scope, strings.TrimSpace(entry.ProductID))
	}
	return s.repo.GetProductBySKUCode(scope, strings.TrimSpace(entry.SKUCode))
}

// AdoptListingVersion 采用 Listing 版本
func (s *Service) AdoptListingVersion(orgID string, userID string, productID string, versionID string) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}

	versions, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return nil, err
	}

	var target *models.EcomListingVersion
	for _, v := range versions {
		if v.ID == versionID {
			target = &v
			break
		}
	}
	if target == nil {
		return nil, nil
	}

	now := time.Now()
	for _, v := range versions {
		if v.ID != versionID && v.Status == models.ListingVersionStatusAdopted {
			v.Status = models.ListingVersionStatusReady
			s.repo.UpdateListingVersion(scope, v)
		}
	}

	target.Status = models.ListingVersionStatusAdopted
	target.AdoptedAt = &now
	updated, err := s.repo.UpdateListingVersion(scope, *target)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingAdopted,
		"Listing Adopted", "Listing version "+target.VersionLabel+" adopted", nil)

	s.updateProductListingStatus(scope, productID)

	s.updateProductMainStatus(scope, productID)

	return updated, nil
}

func (s *Service) BatchAdoptListingVersions(orgID string, userID string, input BatchAdoptListingVersionsInput) (*BatchListingMutationResult, error) {
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("at least 1 adopt item is required")
	}

	result := &BatchListingMutationResult{
		Total: len(input.Items),
		Items: make([]BatchListingMutationItem, 0, len(input.Items)),
	}
	seenProductIDs := make(map[string]struct{}, len(input.Items))

	for _, entry := range input.Items {
		item := BatchListingMutationItem{
			ProductID: entry.ProductID,
			VersionID: entry.VersionID,
		}

		product, err := s.repo.GetProduct(repository.Scope{OrgID: orgID}, entry.ProductID)
		if err == nil && product != nil {
			item.SKUCode = product.SKUCode
			item.ProductTitle = product.Title
		}

		switch {
		case strings.TrimSpace(entry.ProductID) == "":
			item.Message = "product_id is required"
		case strings.TrimSpace(entry.VersionID) == "":
			item.Message = "version_id is required"
		case err != nil:
			item.Message = "product not found"
		default:
			if _, exists := seenProductIDs[entry.ProductID]; exists {
				item.Message = "duplicate product in batch request"
			} else {
				seenProductIDs[entry.ProductID] = struct{}{}
				listing, adoptErr := s.AdoptListingVersion(orgID, userID, entry.ProductID, entry.VersionID)
				if adoptErr != nil {
					item.Message = adoptErr.Error()
				} else if listing == nil {
					item.Message = "listing version not found"
				} else {
					item.Success = true
					item.Message = "listing version adopted"
					item.VersionID = listing.ID
					item.VersionLabel = listing.VersionLabel
					item.Listing = listing
					result.Succeeded++
				}
			}
		}

		if !item.Success {
			result.Failed++
		}
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func (s *Service) UpdateListingVersion(orgID string, userID string, productID string, versionID string, input UpdateListingVersionInput) (*models.EcomListingVersion, error) {
	scope := repository.Scope{UserID: userID, OrgID: orgID}
	product, err := s.repo.GetProduct(scope, productID)
	if err != nil {
		return nil, err
	}

	versions, err := s.repo.ListListingVersions(scope, productID)
	if err != nil {
		return nil, err
	}

	var target *models.EcomListingVersion
	for _, version := range versions {
		if version.ID == versionID {
			current := version
			target = &current
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("listing version not found")
	}

	inputVersionLabel := target.VersionLabel
	inputTitle := target.Title
	inputDescription := target.Description
	inputBulletPoints := []string(target.BulletPoints)
	inputKeywords := []string(target.Keywords)
	inputPlatform := target.Platform
	inputSite := target.Site
	inputLocale := target.Locale
	if input.VersionLabel != nil {
		inputVersionLabel = strings.TrimSpace(*input.VersionLabel)
	}
	if input.Title != nil {
		inputTitle = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		inputDescription = strings.TrimSpace(*input.Description)
	}
	if input.BulletPoints != nil {
		inputBulletPoints = sanitizeStringList(*input.BulletPoints)
	}
	if input.Keywords != nil {
		inputKeywords = sanitizeStringList(*input.Keywords)
	}
	if input.Platform != nil {
		inputPlatform = strings.TrimSpace(*input.Platform)
	}
	if input.Site != nil {
		inputSite = strings.TrimSpace(*input.Site)
	}
	if input.Locale != nil {
		inputLocale = strings.TrimSpace(*input.Locale)
	}

	created, err := s.createListingVersionForProduct(scope, product, CreateListingVersionInput{
		VersionLabel: inputVersionLabel,
		Title:        inputTitle,
		Description:  inputDescription,
		BulletPoints: inputBulletPoints,
		Keywords:     inputKeywords,
		Platform:     inputPlatform,
		Site:         inputSite,
		Locale:       inputLocale,
	}, false)
	if err != nil {
		return nil, err
	}

	s.logActivity(scope, productID, models.ProductActivityTypeListingGenerated,
		"Listing Edited", "New listing version "+created.VersionLabel+" created from "+target.VersionLabel, nil)

	return created, nil
}
