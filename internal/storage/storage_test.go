package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"ecommerce-service/internal/config"

	"gorm.io/gorm"
)

func TestConnectSQLiteAndPingWithTablePrefix(t *testing.T) {
	db, err := ConnectDB(config.DatabaseConfig{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "storage.db"), TablePrefix: "ecommerce_test_", MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("ConnectDB sqlite: %v", err)
	}
	if err := PingDB(context.Background(), db); err != nil {
		t.Fatalf("PingDB sqlite: %v", err)
	}
	if got := db.NamingStrategy.TableName("billing_charge_records"); !strings.HasPrefix(got, "ecommerce_test_") {
		t.Fatalf("table prefix not applied: %s", got)
	}
}

func TestInitDBSQLiteAppliesMigrationsAndKeepsConnectionReusableAfterRollback(t *testing.T) {
	db, err := InitDB(config.DatabaseConfig{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "storage-migrated.db"), TablePrefix: "ecommerce_", AutoMigrateEnabled: true, AllowStartupMigrate: true, MaxOpenConns: 1, MaxIdleConns: 1}, "release")
	if err != nil {
		t.Fatalf("InitDB sqlite migrations: %v", err)
	}
	if err := PingDB(context.Background(), db); err != nil {
		t.Fatalf("PingDB after InitDB: %v", err)
	}

	sentinel := errors.New("rollback sentinel")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE TABLE rollback_probe (id TEXT PRIMARY KEY)").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO rollback_probe (id) VALUES (?)", "inside-tx").Error; err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel rollback error, got %v", err)
	}

	if err := db.Exec("CREATE TABLE rollback_probe (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("transaction cleanup did not rollback DDL/connection is unusable: %v", err)
	}
	if err := db.Exec("INSERT INTO rollback_probe (id) VALUES (?)", "after-rollback").Error; err != nil {
		t.Fatalf("connection unusable after rollback: %v", err)
	}
	if err := PingDB(context.Background(), db); err != nil {
		t.Fatalf("PingDB after rollback: %v", err)
	}
}

func TestAutoMigratePolicyFailsClosedOutsideDevForPostgres(t *testing.T) {
	err := validateAutoMigratePolicy(config.DatabaseConfig{Driver: "postgres", AutoMigrateEnabled: true, AllowStartupMigrate: false}, "release")
	if err == nil || !strings.Contains(err.Error(), "startup auto migrate blocked") {
		t.Fatalf("expected non-dev postgres automigrate to fail closed, got %v", err)
	}
	if err := validateAutoMigratePolicy(config.DatabaseConfig{Driver: "sqlite", AutoMigrateEnabled: true}, "release"); err != nil {
		t.Fatalf("sqlite test/dev automigrate should be allowed: %v", err)
	}
}

func TestPingNilDependenciesAreStable(t *testing.T) {
	if err := PingDB(context.Background(), nil); err == nil {
		t.Fatalf("nil db should be reported not ready")
	}
	if err := PingRedis(context.Background(), nil); err != nil {
		t.Fatalf("disabled redis should be ready: %v", err)
	}
}
