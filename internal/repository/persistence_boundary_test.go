package repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

func TestProductAssetRelationsExportPackagesAndLibraryStayOrgScoped(t *testing.T) {
	db := newTemplateCenterRepositoryTestDB(t)
	productRepo := NewProductCenterRepository(db)
	assetRepo := NewImageRuntimeRepository(db)
	scopeA := Scope{UserID: "user-a", OrgID: "org-a"}
	scopeB := Scope{UserID: "user-b", OrgID: "org-b"}

	productA, err := productRepo.CreateProduct(scopeA, models.EcomProductSKU{ID: "prod-a", SKUCode: "sku-a", Title: "Product A", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusPartial, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending})
	if err != nil {
		t.Fatalf("create product a: %v", err)
	}
	if _, err := productRepo.CreateProduct(scopeB, models.EcomProductSKU{ID: "prod-b", SKUCode: "sku-b", Title: "Product B", Status: models.ProductStatusDraft, AssetStatus: models.AssetStatusPartial, ListingStatus: models.ListingStatusMissing, ExportStatus: models.ExportStatusPending}); err != nil {
		t.Fatalf("create product b: %v", err)
	}
	if err := assetRepo.CreateAsset(&models.EcommerceAsset{ID: "asset-a", OrganizationID: scopeA.OrgID, UserID: scopeA.UserID, AssetType: "image", SourceType: "upload", StorageKey: "private/org-a/raw/object.png", MimeType: "image/png", FileName: "hero-a.png", Metadata: `{"status":"ready","tags":["hero"]}`}); err != nil {
		t.Fatalf("create asset a: %v", err)
	}
	if err := assetRepo.CreateAsset(&models.EcommerceAsset{ID: "asset-b", OrganizationID: scopeB.OrgID, UserID: scopeB.UserID, AssetType: "image", SourceType: "upload", StorageKey: "private/org-b/raw/object.png", MimeType: "image/png", FileName: "hero-b.png", Metadata: `{"status":"ready","tags":["hero"]}`}); err != nil {
		t.Fatalf("create asset b: %v", err)
	}

	relA, err := productRepo.AddProductAsset(scopeA, models.EcomAssetRelation{ID: "rel-a", AssetID: "asset-a", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: productA.ID, RelationType: models.AssetRelationTypePrimary, AssetRole: models.AssetRoleHero, IsPrimary: true, PlatformCode: "amazon", SiteCode: "us", LocaleCode: "en", Visibility: "library", Metadata: `{"status":"ready","tags":["hero"]}`})
	if err != nil {
		t.Fatalf("add product asset a: %v", err)
	}
	if _, err := productRepo.AddProductAsset(scopeB, models.EcomAssetRelation{ID: "rel-b", AssetID: "asset-b", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: "prod-b", RelationType: models.AssetRelationTypePrimary, AssetRole: models.AssetRoleHero, IsPrimary: true, Visibility: "library", Metadata: `{"status":"ready"}`}); err != nil {
		t.Fatalf("add product asset b: %v", err)
	}
	secondary, err := productRepo.AddProductAsset(scopeA, models.EcomAssetRelation{ID: "rel-a-secondary", AssetID: "asset-a", OwnerType: models.AssetRelationOwnerTypeProduct, OwnerID: productA.ID, RelationType: models.AssetRelationTypeSource, AssetRole: models.AssetRoleDetailShot, IsPrimary: true, SortOrder: 2, Visibility: "private", Metadata: `{"status":"ready","tags":["detail"]}`})
	if err != nil {
		t.Fatalf("add secondary asset relation: %v", err)
	}

	assets, err := productRepo.ListProductAssets(scopeA, productA.ID)
	if err != nil || len(assets) != 2 {
		t.Fatalf("list product assets scoped to org-a: len=%d err=%v assets=%+v", len(assets), err, assets)
	}
	if _, err := productRepo.GetProductAssetRelation(scopeB, productA.ID, relA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a product relation, got err=%v", err)
	}
	foundRel, err := productRepo.FindProductAssetRelation(scopeA, productA.ID, "asset-a")
	if err != nil || foundRel.ID == "" {
		t.Fatalf("find product asset relation: rel=%+v err=%v", foundRel, err)
	}
	if err := productRepo.ClearPrimaryProductAssets(scopeA, productA.ID, relA.ID); err != nil {
		t.Fatalf("clear primary assets: %v", err)
	}
	cleared, err := productRepo.GetProductAssetRelation(scopeA, productA.ID, secondary.ID)
	if err != nil || cleared.IsPrimary {
		t.Fatalf("secondary primary flag not cleared: rel=%+v err=%v", cleared, err)
	}
	if err := productRepo.DeleteProductAsset(scopeB, relA.ID); err != nil {
		t.Fatalf("cross-org delete product asset should be no-op: %v", err)
	}
	if _, err := productRepo.GetProductAssetRelation(scopeA, productA.ID, relA.ID); err != nil {
		t.Fatalf("cross-org delete removed org-a relation: %v", err)
	}
	count, err := productRepo.GetProductAssetCount(scopeA, productA.ID)
	if err != nil || count != 2 {
		t.Fatalf("asset count = %d err=%v, want 2", count, err)
	}

	library, total, err := productRepo.ListAssetLibrary(scopeA, AssetLibraryFilter{SKUCode: "sku-a", SourceType: "upload", AssetRole: models.AssetRoleHero, Visibility: "library", Status: "ready", Query: "hero-a", Limit: 10})
	if err != nil || total != 1 || len(library) != 1 || library[0].RelationID != relA.ID || strings.Contains(library[0].StorageKey, "org-b") {
		t.Fatalf("asset library leaked scope or missed relation: total=%d items=%+v err=%v", total, library, err)
	}
	stats, err := productRepo.AssetLibraryStats(scopeA, AssetLibraryFilter{ProductID: productA.ID}, "source_type")
	if err != nil || len(stats) != 1 || stats[0].Key != "upload" || stats[0].Count != 2 {
		t.Fatalf("asset library stats mismatch: %+v err=%v", stats, err)
	}
	if _, err := productRepo.GetAssetLibraryRelation(scopeB, relA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a library relation, got %v", err)
	}
}

func TestProductExportPackageManifestAndListingBoundaries(t *testing.T) {
	db := newTemplateCenterRepositoryTestDB(t)
	repo := NewProductCenterRepository(db)
	scopeA := Scope{UserID: "user-a", OrgID: "org-a"}
	scopeB := Scope{UserID: "user-b", OrgID: "org-b"}
	adoptedAt := time.Now().UTC()

	if _, err := repo.CreateProduct(scopeA, models.EcomProductSKU{ID: "prod-export", SKUCode: "sku-export", Title: "Export Product", Status: models.ProductStatusListingReady, AssetStatus: models.AssetStatusReady, ListingStatus: models.ListingStatusReady, ExportStatus: models.ExportStatusReady}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	listing, err := repo.CreateListingVersion(scopeA, models.EcomListingVersion{ID: "listing-1", ProductID: "prod-export", VersionNo: 1, VersionLabel: "v1", Status: models.ListingVersionStatusAdopted, Title: "Listing", Platform: "amazon", Site: "us", Locale: "en", AdoptedAt: &adoptedAt})
	if err != nil {
		t.Fatalf("create listing: %v", err)
	}
	if next, err := repo.GetNextListingVersionNo(scopeA, "prod-export"); err != nil || next != 2 {
		t.Fatalf("next listing version = %d err=%v, want 2", next, err)
	}
	if adopted, err := repo.GetAdoptedListingVersion(scopeA, "prod-export", "amazon", "us", "en"); err != nil || adopted.ID != listing.ID {
		t.Fatalf("adopted listing mismatch: %+v err=%v", adopted, err)
	}
	if ok, err := repo.HasAdoptedListingVersion(scopeA, "prod-export"); err != nil || !ok {
		t.Fatalf("HasAdoptedListingVersion = %v err=%v, want true", ok, err)
	}

	pkg, err := repo.CreateExportPackage(scopeA, models.EcomExportPackage{ID: "pkg-export", Status: models.ExportPackageStatusPartialSucceeded, Platform: "amazon", Site: "us", Locale: "en", Format: "zip", Schema: "amazon-flat-file", TotalCount: 2, SucceededCount: 1, FailedCount: 1, PackageManifest: `{"items":[{"sku":"sku-export","taskId":"task-export"}],"downloadUrl":"https://cdn.example/download/pkg-export.zip"}`})
	if err != nil {
		t.Fatalf("create export package: %v", err)
	}
	if strings.Contains(pkg.PackageManifest, "storage_key") || strings.Contains(pkg.PackageManifest, "private/") {
		t.Fatalf("package manifest should stay public, got %s", pkg.PackageManifest)
	}
	if _, err := repo.CreateExportTask(scopeA, models.EcomExportTask{ID: "task-export", ProductID: "prod-export", PackageID: pkg.ID, Status: models.ExportTaskStatusSucceeded, Platform: "amazon", Site: "us", Locale: "en", Format: "zip", ListingVersionID: listing.ID, ListingVersionLabel: listing.VersionLabel, PrimaryAssetRole: models.AssetRoleHero, AssetCount: 1, AssetManifest: `[{"assetId":"asset-1"}]`, StorageKey: "private/org-a/exports/task.zip", PackageURL: "https://cdn.example/task.zip"}); err != nil {
		t.Fatalf("create export task: %v", err)
	}
	if packages, err := repo.ListExportPackages(scopeA); err != nil || len(packages) != 1 || packages[0].ID != pkg.ID {
		t.Fatalf("list export packages: %+v err=%v", packages, err)
	}
	if _, err := repo.GetExportPackage(scopeB, pkg.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a package, got err=%v", err)
	}
	pkg.Status = models.ExportPackageStatusSucceeded
	pkg.SucceededCount = 2
	updated, err := repo.UpdateExportPackage(scopeA, *pkg)
	if err != nil || updated.Status != models.ExportPackageStatusSucceeded || updated.OrganizationID != scopeA.OrgID {
		t.Fatalf("update package mismatch: %+v err=%v", updated, err)
	}
	if tasks, err := repo.ListExportTasksByPackage(scopeA, pkg.ID); err != nil || len(tasks) != 1 || tasks[0].StorageKey == "" {
		t.Fatalf("package child task mismatch: %+v err=%v", tasks, err)
	}
	if tasks, err := repo.ListExportTasksByPackage(scopeB, pkg.ID); err != nil || len(tasks) != 0 {
		t.Fatalf("org-b saw org-a package tasks: %+v err=%v", tasks, err)
	}
	if ok, err := repo.HasSuccessfulExportTask(scopeA, "prod-export"); err != nil || !ok {
		t.Fatalf("HasSuccessfulExportTask = %v err=%v, want true", ok, err)
	}
	if all, err := repo.ListAllExportTasks(scopeA); err != nil || len(all) != 1 {
		t.Fatalf("list all export tasks: %+v err=%v", all, err)
	}
	if _, err := repo.GetExportTask(scopeB, "task-export"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a export task, got err=%v", err)
	}
	listing.Status = models.ListingVersionStatusReady
	if _, err := repo.UpdateListingVersion(scopeA, *listing); err != nil {
		t.Fatalf("update listing: %v", err)
	}
	versions, err := repo.ListListingVersions(scopeA, "prod-export")
	if err != nil || len(versions) != 1 || versions[0].Status != models.ListingVersionStatusReady {
		t.Fatalf("list listing versions mismatch: %+v err=%v", versions, err)
	}
}

func TestVisualWorkflowRepositoryScopesIdempotencyAndRollsBackFailedReplace(t *testing.T) {
	db := newTemplateCenterRepositoryTestDB(t)
	repo := NewVisualWorkflowRepository(db)
	now := time.Now().UTC()

	sessionA := &models.EcommerceVisualWorkflowSession{ID: "session-a", OrganizationID: "org-a", UserID: "user-a", ProductID: "prod-a", SKUCode: "sku-a", ToolSlug: "ai-product", CurrentStage: models.VisualWorkflowStageSource, Status: models.VisualWorkflowStatusDraft, ReadinessJSON: `{}`, IntentSpecJSON: `{}`, PromptPlanJSON: `{}`, GenerationVersionsJSON: `[{"id":"gen-v1"}]`, IdempotencyKey: "idem-a", Metadata: `{"generationVersion":"gen-v1"}`, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateSession(sessionA); err != nil {
		t.Fatalf("create session a: %v", err)
	}
	if err := repo.CreateSession(&models.EcommerceVisualWorkflowSession{ID: "session-b", OrganizationID: "org-b", UserID: "user-b", ProductID: "prod-b", SKUCode: "sku-b", CurrentStage: models.VisualWorkflowStageSource, Status: models.VisualWorkflowStatusDraft, ReadinessJSON: `{}`, IntentSpecJSON: `{}`, PromptPlanJSON: `{}`, GenerationVersionsJSON: `[]`, IdempotencyKey: "idem-b", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create session b: %v", err)
	}
	if _, err := repo.GetSession("org-b", "session-a"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a session, got %v", err)
	}
	if found, err := repo.FindSessionByIdempotencyKey("org-a", "idem-a"); err != nil || found.ID != "session-a" {
		t.Fatalf("idempotency scoped lookup mismatch: %+v err=%v", found, err)
	}
	if items, err := repo.ListSessions("org-a", VisualWorkflowSessionFilter{ProductID: "prod-a", SKUCode: "sku-a", Status: models.VisualWorkflowStatusDraft, Limit: -1, Offset: -5}); err != nil || len(items) != 1 || items[0].ID != "session-a" {
		t.Fatalf("list sessions scoped mismatch: %+v err=%v", items, err)
	}
	if found, err := repo.FindSessionByGenerationVersionID("gen-v1"); err != nil || found.ID != "session-a" {
		t.Fatalf("find session by generation version: %+v err=%v", found, err)
	}
	if found, err := repo.FindSessionByMetadataLike("generationVersion"); err != nil || found.ID != "session-a" {
		t.Fatalf("find session by metadata: %+v err=%v", found, err)
	}

	if err := repo.CreateSourceReference(&models.EcommerceVisualSourceReference{ID: "src-active", OrganizationID: "org-a", UserID: "user-a", SessionID: "session-a", ProductID: "prod-a", SKUCode: "sku-a", SourceKind: models.VisualSourceKindUpload, StorageKey: "private/org-a/source.png", MimeType: "image/png", Status: models.VisualSourceStatusReady, ResolveStatus: models.VisualReadinessReady, CreatedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("create active source: %v", err)
	}
	if err := repo.CreateSourceReference(&models.EcommerceVisualSourceReference{ID: "src-archived", OrganizationID: "org-a", UserID: "user-a", SessionID: "session-a", ProductID: "prod-a", SKUCode: "sku-a", SourceKind: models.VisualSourceKindUpload, Status: models.VisualSourceStatusArchived, ResolveStatus: models.VisualReadinessMissing, CreatedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("create archived source: %v", err)
	}
	if refs, err := repo.ListSourceReferences("org-a", "session-a"); err != nil || len(refs) != 1 || refs[0].ID != "src-active" {
		t.Fatalf("source references should exclude archived: %+v err=%v", refs, err)
	}
	if latest, err := repo.LatestSourceReference("org-a", "session-a"); err != nil || latest.ID != "src-active" {
		t.Fatalf("latest source mismatch: %+v err=%v", latest, err)
	}
	if _, err := repo.GetSourceReference("org-b", "session-a", "src-active"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a source reference, got %v", err)
	}

	job := &models.EcommerceVisualDeconstructionJob{ID: "job-a", OrganizationID: "org-a", UserID: "user-a", SessionID: "session-a", ProductID: "prod-a", SKUCode: "sku-a", SourceReferenceID: "src-active", Status: models.VisualDeconstructionStatusQueued, Stage: "queued", InputManifestJSON: `{}`, OutputManifestJSON: `{}`, IdempotencyKey: "job-idem"}
	if err := repo.CreateDeconstructionJob(job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	duplicate := &models.EcommerceVisualDeconstructionJob{ID: "job-duplicate", OrganizationID: "org-a", UserID: "user-a", SessionID: "session-a", ProductID: "prod-a", SKUCode: "sku-a", Status: models.VisualDeconstructionStatusQueued, InputManifestJSON: `{}`, OutputManifestJSON: `{}`, IdempotencyKey: "job-idem"}
	if err := repo.CreateDeconstructionJob(duplicate); err != nil {
		t.Fatalf("idempotent duplicate job create: %v", err)
	}
	if duplicate.ID != "job-a" {
		t.Fatalf("duplicate idempotency did not return existing job: %+v", duplicate)
	}
	if _, err := repo.GetDeconstructionJob("org-b", "session-a", "job-a"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a job, got %v", err)
	}
	if latest, err := repo.LatestDeconstructionJob("org-a", "session-a"); err != nil || latest.ID != "job-a" {
		t.Fatalf("latest job mismatch: %+v err=%v", latest, err)
	}

	original := models.EcommerceVisualDeconstructionElement{ID: "elem-original", OrganizationID: "org-a", SessionID: "session-a", JobID: "job-a", ProductID: "prod-a", SKUCode: "sku-a", ElementType: "logo", ElementKey: "logo", ValueJSON: `{}`, Readiness: models.VisualReadinessReady, Selected: true, SortOrder: 1}
	if err := repo.ReplaceDeconstructionElements("org-a", "session-a", "job-a", []models.EcommerceVisualDeconstructionElement{original}); err != nil {
		t.Fatalf("replace original elements: %v", err)
	}
	badElements := []models.EcommerceVisualDeconstructionElement{
		{ID: "elem-dup", OrganizationID: "org-a", SessionID: "session-a", JobID: "job-a", ProductID: "prod-a", SKUCode: "sku-a", ElementType: "title", ValueJSON: `{}`, Readiness: models.VisualReadinessPartial},
		{ID: "elem-dup", OrganizationID: "org-a", SessionID: "session-a", JobID: "job-a", ProductID: "prod-a", SKUCode: "sku-a", ElementType: "title", ValueJSON: `{}`, Readiness: models.VisualReadinessPartial},
	}
	job.Status = models.VisualDeconstructionStatusCompleted
	if err := repo.SaveDeconstructionResult(job, badElements, models.VisualWorkflowStatusReady); err == nil {
		t.Fatalf("expected duplicate element insert to fail")
	}
	remaining, err := repo.ListDeconstructionElements("org-a", "session-a")
	if err != nil || len(remaining) != 1 || remaining[0].ID != original.ID {
		t.Fatalf("failed result did not rollback element replacement: %+v err=%v", remaining, err)
	}
	refreshedSession, err := repo.GetSession("org-a", "session-a")
	if err != nil || refreshedSession.Status != models.VisualWorkflowStatusDraft {
		t.Fatalf("failed result should not update session: %+v err=%v", refreshedSession, err)
	}

	good := []models.EcommerceVisualDeconstructionElement{{ID: "elem-good", OrganizationID: "org-a", SessionID: "session-a", JobID: "job-a", ProductID: "prod-a", SKUCode: "sku-a", ElementType: "title", ValueJSON: `{"text":"safe"}`, Readiness: models.VisualReadinessReady, Selected: true, SortOrder: 1}}
	if err := repo.SaveDeconstructionResult(job, good, models.VisualWorkflowStatusReady); err != nil {
		t.Fatalf("save good result: %v", err)
	}
	if elems, err := repo.ListDeconstructionElements("org-a", "session-a"); err != nil || len(elems) != 1 || elems[0].ID != "elem-good" {
		t.Fatalf("good result elements mismatch: %+v err=%v", elems, err)
	}
	if err := repo.UpdateDeconstructionElement(&models.EcommerceVisualDeconstructionElement{ID: "elem-good", OrganizationID: "org-a", SessionID: "session-a", JobID: "job-a", ProductID: "prod-a", SKUCode: "sku-a", ElementType: "title", ValueJSON: `{"text":"updated"}`, Readiness: models.VisualReadinessReady, Confirmed: true}); err != nil {
		t.Fatalf("update element: %v", err)
	}
	updatedElem, err := repo.GetDeconstructionElement("org-a", "session-a", "elem-good")
	if err != nil || !updatedElem.Confirmed {
		t.Fatalf("updated element mismatch: %+v err=%v", updatedElem, err)
	}
}

func TestImageRuntimeRepositoryPersistsScopedAssetsAndJobBindings(t *testing.T) {
	db := newTemplateCenterRepositoryTestDB(t)
	repo := NewImageRuntimeRepository(db)
	now := time.Now().UTC()
	job := &models.EcommerceImageJob{ID: "img-job", OrganizationID: "org-a", UserID: "user-a", SceneType: "product_hero", InputMode: "text_to_image", Status: "queued", Stage: "queued", Progress: 0, Metadata: `{}`, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateJob(job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := repo.UpdateJobRuntimeBinding(job.ID, "runtime-job-1", "provider-job-1", `{"request_id":"req-1"}`); err != nil {
		t.Fatalf("update runtime binding: %v", err)
	}
	foundJob, err := repo.FindJobByID(job.ID)
	if err != nil || foundJob.RuntimeJobID != "runtime-job-1" || foundJob.ProviderJobID != "provider-job-1" {
		t.Fatalf("runtime binding mismatch: %+v err=%v", foundJob, err)
	}
	foundJob.Status = "succeeded"
	foundJob.Progress = 100
	if err := repo.SaveJob(foundJob); err != nil {
		t.Fatalf("save job: %v", err)
	}
	jobs, err := repo.ListJobs("org-a", "user-a", "product_hero", 0)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "succeeded" {
		t.Fatalf("list jobs mismatch: %+v err=%v", jobs, err)
	}

	asset := &models.EcommerceAsset{ID: "asset-generated", OrganizationID: "org-a", UserID: "user-a", AssetType: "image", SourceType: "generated", StorageKey: "private/org-a/generated.png", MimeType: "image/png", FileName: "generated.png", Metadata: `{"generation_result_key":"img-job:2"}`}
	if err := repo.CreateAsset(asset); err != nil {
		t.Fatalf("create generated asset: %v", err)
	}
	if _, err := repo.FindAssetByID("org-b", asset.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a asset, got %v", err)
	}
	if byID, err := repo.FindAssetByID("org-a", asset.ID); err != nil || byID.StorageKey != asset.StorageKey {
		t.Fatalf("find asset by id mismatch: %+v err=%v", byID, err)
	}
	if byGlobal, err := repo.FindAssetByIDGlobal(asset.ID); err != nil || byGlobal.ID != asset.ID {
		t.Fatalf("find asset global mismatch: %+v err=%v", byGlobal, err)
	}
	if byKey, err := repo.FindAssetByStorageKey("org-a", asset.StorageKey); err != nil || byKey.ID != asset.ID {
		t.Fatalf("find asset by storage key mismatch: %+v err=%v", byKey, err)
	}
	if generated, err := repo.FindGeneratedAssetByJobVariant("org-a", job.ID, 2); err != nil || generated.ID != asset.ID {
		t.Fatalf("find generated asset by variant mismatch: %+v err=%v", generated, err)
	}
}

func TestCommercialRepositoryOutboxPaymentFulfillmentAndPromotionBoundaries(t *testing.T) {
	repo, _ := newCommercialRepositoryTestDB(t)
	now := time.Now().UTC()
	order := &models.CommercialOrder{ID: "order-a", UserID: "user-a", OrganizationID: "org-a", ProductCode: "ecommerce", SKUCode: "sku-a", PackageCode: "pkg-sub", PackageType: "subscription", Currency: "CNY", Quantity: 1, UnitAmount: 100, TotalAmount: 100, Status: "pending_payment", PaymentStatus: "pending", FulfillmentStatus: "pending"}
	if err := repo.CreateOrder(order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	order.Status = "fulfilled"
	order.PaymentStatus = "succeeded"
	order.FulfillmentStatus = "succeeded"
	order.FulfilledAt = &now
	if err := repo.SaveOrder(order); err != nil {
		t.Fatalf("save order: %v", err)
	}
	if latest, err := repo.FindLatestFulfilledSubscriptionOrder("org-a", "pkg-sub"); err != nil || latest.ID != order.ID {
		t.Fatalf("latest subscription order mismatch: %+v err=%v", latest, err)
	}
	if _, err := repo.FindLatestFulfilledSubscriptionOrder("org-b", "pkg-sub"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("org-b should not read org-a fulfilled order, got %v", err)
	}
	payment := &models.CommercialPayment{ID: "pay-a", OrderID: order.ID, UserID: "user-a", OrganizationID: "org-a", Amount: 100, Currency: "CNY", Status: "succeeded", PaidAt: &now}
	if err := repo.CreatePayment(payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if latest, err := repo.FindLatestPaymentByOrderID(order.ID); err != nil || latest.ID != payment.ID {
		t.Fatalf("latest payment mismatch: %+v err=%v", latest, err)
	}
	fulfillment := &models.CommercialFulfillment{ID: "ful-a", OrderID: order.ID, UserID: "user-a", OrganizationID: "org-a", PackageCode: "pkg-sub", FulfillmentMode: "wallet_allowance", Status: "succeeded", FulfilledAt: &now}
	if err := repo.CreateFulfillment(fulfillment); err != nil {
		t.Fatalf("create fulfillment: %v", err)
	}
	if latest, err := repo.FindLatestFulfillmentByOrderID(order.ID); err != nil || latest.ID != fulfillment.ID {
		t.Fatalf("latest fulfillment mismatch: %+v err=%v", latest, err)
	}
	charge := &models.BillingChargeRecord{ID: "charge-a", ProductCode: "ecommerce", OrganizationID: "org-a", EventID: "event-a", BusinessType: "runtime", NetAmount: 12, Status: "settled", OccurredAt: now}
	if err := repo.CreateBillingChargeRecord(charge); err != nil {
		t.Fatalf("create charge: %v", err)
	}
	charge.ChannelStatus = "sent"
	if err := repo.UpdateBillingChargeRecord(charge); err != nil {
		t.Fatalf("update charge: %v", err)
	}
	if found, err := repo.GetBillingChargeRecord(charge.ID); err != nil || found.ChannelStatus != "sent" {
		t.Fatalf("get charge mismatch: %+v err=%v", found, err)
	}
	promo := &models.PromotionAttributionAttempt{ID: "promo-a", ProductCode: "ecommerce", OrganizationID: "org-a", UserID: "user-a", PromotionCode: "PROMO", TriggerType: "checkout", ReferenceType: "order", ReferenceID: order.ID, Status: "pending"}
	if err := repo.CreatePromotionAttempt(promo); err != nil {
		t.Fatalf("create promo: %v", err)
	}
	promo.Status = "applied"
	if err := repo.UpdatePromotionAttempt(promo); err != nil {
		t.Fatalf("update promo: %v", err)
	}
	if err := repo.CreateOutboxEvent(&models.CommercialEventOutbox{ID: "outbox-ready", ProductCode: "ecommerce", OrganizationID: "org-a", UserID: "user-a", EventType: "order.fulfilled", AggregateType: "order", AggregateID: order.ID, Status: "pending", PayloadJSON: `{"orderId":"order-a"}`, AvailableAt: now.Add(-time.Minute)}); err != nil {
		t.Fatalf("create ready outbox: %v", err)
	}
	if err := repo.CreateOutboxEvent(&models.CommercialEventOutbox{ID: "outbox-later", ProductCode: "ecommerce", OrganizationID: "org-a", EventType: "order.fulfilled", AggregateType: "order", AggregateID: order.ID, Status: "pending", PayloadJSON: `{}`, AvailableAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("create later outbox: %v", err)
	}
	replayable, err := repo.ListReplayableOutbox(0, now)
	if err != nil || len(replayable) != 1 || replayable[0].ID != "outbox-ready" {
		t.Fatalf("replayable outbox mismatch: %+v err=%v", replayable, err)
	}
	replayable[0].Status = "sent"
	if err := repo.UpdateOutboxEvent(&replayable[0]); err != nil {
		t.Fatalf("update outbox: %v", err)
	}
}
