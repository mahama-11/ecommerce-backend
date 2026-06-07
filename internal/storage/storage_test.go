package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ecommerce-service/internal/config"
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
