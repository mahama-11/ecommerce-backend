package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ecommerce-service/internal/config"
	"ecommerce-service/internal/migration"

	"github.com/go-redis/redis/v8"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func InitDB(cfg config.DatabaseConfig, ginMode string) (*gorm.DB, error) {
	db, err := ConnectDB(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.AutoMigrateEnabled {
		if err := validateAutoMigratePolicy(cfg, ginMode); err != nil {
			return nil, err
		}
		if err := migration.Up(db, cfg); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func ConnectDB(cfg config.DatabaseConfig) (*gorm.DB, error) {
	newLogger := gormlogger.New(
		log.New(os.Stdout, "", log.LstdFlags),
		gormlogger.Config{SlowThreshold: time.Second, LogLevel: gormlogger.Info, IgnoreRecordNotFoundError: true, Colorful: false},
	)
	var (
		db  *gorm.DB
		err error
	)
	switch cfg.Driver {
	case "sqlite":
		if mkdirErr := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); mkdirErr != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", mkdirErr)
		}
		db, err = gorm.Open(sqlite.Open(cfg.SQLitePath), &gorm.Config{Logger: newLogger, NamingStrategy: namingStrategy{NamingStrategy: schema.NamingStrategy{TablePrefix: cfg.TablePrefix}}})
	default:
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode)
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: newLogger, NamingStrategy: namingStrategy{NamingStrategy: schema.NamingStrategy{TablePrefix: cfg.TablePrefix}}})
	}
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

func PingDB(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func validateAutoMigratePolicy(cfg config.DatabaseConfig, ginMode string) error {
	if !cfg.AutoMigrateEnabled {
		return nil
	}
	if strings.EqualFold(cfg.Driver, "sqlite") || strings.EqualFold(ginMode, "debug") || cfg.AllowStartupMigrate {
		return nil
	}
	return fmt.Errorf("startup auto migrate blocked for driver=%s gin_mode=%s", cfg.Driver, ginMode)
}

func InitRedis(cfg config.RedisConfig) (*redis.Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), Password: cfg.Password, DB: cfg.DB, PoolSize: cfg.PoolSize, MinIdleConns: cfg.MinIdleConns, MaxRetries: cfg.MaxRetries, DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return client, nil
}

func PingRedis(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Ping(ctx).Err()
}

type namingStrategy struct{ schema.NamingStrategy }

func (s namingStrategy) TableName(str string) string {
	base := s.NamingStrategy.TableName(str)
	if s.TablePrefix == "" {
		return base
	}
	base = strings.TrimPrefix(base, s.TablePrefix)
	base = strings.TrimPrefix(base, "ecommerce_")
	return s.TablePrefix + base
}
