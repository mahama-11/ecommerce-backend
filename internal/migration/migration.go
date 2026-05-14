package migration

import (
	"fmt"
	"sort"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/models"

	"gorm.io/gorm"
)

type Step struct {
	Version int64
	Name    string
	Up      func(*gorm.DB) error
}

type Status struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt *time.Time
}

type record struct {
	Version   int64     `gorm:"primaryKey;autoIncrement:false"`
	Name      string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func Steps(cfg config.DatabaseConfig) []Step {
	steps := []Step{{
		Version: 202604230001,
		Name:    "baseline_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.AuditLog{},
				&models.UserPreference{},
				&models.Activity{},
				&models.SavedTemplate{},
				&models.WorkflowEvent{},
				&models.LinkedDesignAsset{},
				&models.LinkedDelivery{},
				&models.TemplateBridge{},
			)
		},
	}, {
		Version: 202604240001,
		Name:    "template_center_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.TemplateCatalog{},
				&models.TemplateCatalogLocale{},
				&models.TemplateCatalogVersion{},
				&models.TemplateCatalogSchema{},
				&models.TemplateCatalogExample{},
				&models.TemplateFavorite{},
				&models.TemplateInstance{},
				&models.TemplateUsageEvent{},
			)
		},
	}, {
		Version: 202604240002,
		Name:    "template_center_managed_source_backfill",
		Up: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&models.TemplateCatalog{}); err != nil {
				return err
			}
			return db.Model(&models.TemplateCatalog{}).
				Where("scope = ? AND owner_team = ? AND created_by = ? AND (managed_source = ? OR managed_source IS NULL)", "official", "agent-ecommerce", "system", "").
				Update("managed_source", "seed_builtin").Error
		},
	}, {
		Version: 202604240003,
		Name:    "image_runtime_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.EcommerceImageJob{},
				&models.EcommerceAsset{},
			)
		},
	}, {
		Version: 202604260001,
		Name:    "commercial_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.PromotionAttributionAttempt{},
				&models.BillingChargeRecord{},
				&models.CommercialEventOutbox{},
				&models.CommercialOrder{},
				&models.CommercialPayment{},
				&models.CommercialFulfillment{},
			)
		},
	}, {
		Version: 202604250001,
		Name:    "template_center_example_asset_fields",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(&models.TemplateCatalogExample{})
		},
	}, {
		Version: 202604250002,
		Name:    "template_center_seed_builtin_drift_backfill",
		Up: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&models.TemplateCatalog{}); err != nil {
				return err
			}
			return db.Model(&models.TemplateCatalog{}).
				Where("scope = ? AND owner_team = ? AND created_by = ? AND managed_source = ?", "official", "agent-ecommerce", "system", "ops_manual").
				Update("managed_source", "seed_builtin").Error
		},
	}, {
		Version: 202604280001,
		Name:    "commercial_order_schema_extend",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.CommercialOrder{},
				&models.CommercialPayment{},
				&models.CommercialFulfillment{},
			)
		},
	}, {
		Version: 202604290001,
		Name:    "productcenter_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.EcomProductSKU{},
				&models.EcomAssetRelation{},
				&models.EcomListingVersion{},
				&models.EcomProfitSnapshot{},
				&models.EcomExportTask{},
				&models.EcomExportPackage{},
				&models.EcomProductActivity{},
			)
		},
	}, {
		Version: 202604300001,
		Name:    "prompt_center_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.EcommercePromptRun{},
				&models.EcommerceImageJob{},
			)
		},
	}, {
		Version: 202605080001,
		Name:    "asset_library_relation_governance_fields",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(&models.EcomAssetRelation{})
		},
	}, {
		Version: 202605080002,
		Name:    "export_package_group_schema",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(&models.EcomExportTask{}, &models.EcomExportPackage{})
		},
	}, {
		Version: 202605140001,
		Name:    "visual_workflow_schema_bootstrap",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&models.EcommerceVisualWorkflowSession{},
				&models.EcommerceVisualSourceReference{},
				&models.EcommerceVisualDeconstructionJob{},
				&models.EcommerceVisualDeconstructionElement{},
			)
		},
	}, {
		Version: 202605140002,
		Name:    "visual_workflow_intent_prompt_plan_columns",
		Up: func(db *gorm.DB) error {
			return db.AutoMigrate(&models.EcommerceVisualWorkflowSession{})
		},
	}}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Version < steps[j].Version })
	return steps
}

func Up(db *gorm.DB, cfg config.DatabaseConfig) error {
	if err := ensureMetadataTable(db, cfg.TablePrefix); err != nil {
		return err
	}
	applied, err := appliedVersions(db, cfg.TablePrefix)
	if err != nil {
		return err
	}
	for _, step := range Steps(cfg) {
		if _, ok := applied[step.Version]; ok {
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := step.Up(tx); err != nil {
				return err
			}
			return tx.Table(metadataTable(cfg.TablePrefix)).Create(&record{Version: step.Version, Name: step.Name, AppliedAt: time.Now()}).Error
		}); err != nil {
			return fmt.Errorf("apply migration %d_%s: %w", step.Version, step.Name, err)
		}
	}
	return nil
}

func CurrentVersion(db *gorm.DB, cfg config.DatabaseConfig) (int64, error) {
	if err := ensureMetadataTable(db, cfg.TablePrefix); err != nil {
		return 0, err
	}
	var item record
	err := db.Table(metadataTable(cfg.TablePrefix)).Order("version desc").First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return item.Version, nil
}

func ListStatus(db *gorm.DB, cfg config.DatabaseConfig) ([]Status, error) {
	if err := ensureMetadataTable(db, cfg.TablePrefix); err != nil {
		return nil, err
	}
	var records []record
	if err := db.Table(metadataTable(cfg.TablePrefix)).Order("version asc").Find(&records).Error; err != nil {
		return nil, err
	}
	byVersion := make(map[int64]record, len(records))
	for _, item := range records {
		byVersion[item.Version] = item
	}
	out := make([]Status, 0, len(Steps(cfg)))
	for _, step := range Steps(cfg) {
		status := Status{Version: step.Version, Name: step.Name}
		if item, ok := byVersion[step.Version]; ok {
			status.Applied = true
			appliedAt := item.AppliedAt
			status.AppliedAt = &appliedAt
		}
		out = append(out, status)
	}
	return out, nil
}

func ensureMetadataTable(db *gorm.DB, tablePrefix string) error {
	return db.Table(metadataTable(tablePrefix)).AutoMigrate(&record{})
}

func appliedVersions(db *gorm.DB, tablePrefix string) (map[int64]struct{}, error) {
	var records []record
	if err := db.Table(metadataTable(tablePrefix)).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]struct{}, len(records))
	for _, item := range records {
		out[item.Version] = struct{}{}
	}
	return out, nil
}

func metadataTable(tablePrefix string) string {
	return tablePrefix + "schema_migrations"
}
