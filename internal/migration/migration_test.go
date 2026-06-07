package migration

import (
	"path/filepath"
	"testing"

	"ecommerce-service/internal/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrationUpIsSchemaSmokeAndIdempotentOnSQLite(t *testing.T) {
	cfg := config.DatabaseConfig{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "migration.db"), TablePrefix: "ecommerce_test_"}
	db, err := gorm.Open(sqlite.Open(cfg.SQLitePath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Up(db, cfg); err != nil {
		t.Fatalf("first migration up: %v", err)
	}
	if err := Up(db, cfg); err != nil {
		t.Fatalf("second migration up should be idempotent: %v", err)
	}
	status, err := ListStatus(db, cfg)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	if len(status) == 0 {
		t.Fatalf("expected migration status records")
	}
	for _, item := range status {
		if !item.Applied || item.AppliedAt == nil {
			t.Fatalf("migration not marked applied: %+v", item)
		}
	}
}
