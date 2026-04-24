package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Host       string           `mapstructure:"host"`
	Port       int              `mapstructure:"port"`
	GinMode    string           `mapstructure:"gin_mode"`
	LogLevel   string           `mapstructure:"log_level"`
	UseMock    bool             `mapstructure:"use_mock"`
	App        AppConfig        `mapstructure:"app"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Security   SecurityConfig   `mapstructure:"security"`
	Platform   PlatformConfig   `mapstructure:"platform"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
}

type AppConfig struct {
	FrontendBaseURL string             `mapstructure:"frontend_base_url"`
	ProductName     string             `mapstructure:"product_name"`
	ProductCode     string             `mapstructure:"product_code"`
	DefaultLanguage string             `mapstructure:"default_language"`
	ImageRuntime    ImageRuntimeConfig `mapstructure:"image_runtime"`
}

type ImageRuntimeConfig struct {
	GlobalNegativePrompt string                             `mapstructure:"global_negative_prompt"`
	ScenePromptPolicies  map[string]ScenePromptPolicyConfig `mapstructure:"scene_prompt_policies"`
}

type ScenePromptPolicyConfig struct {
	ToolSlug              string `mapstructure:"tool_slug"`
	DisplayName           string `mapstructure:"display_name"`
	SystemPrompt          string `mapstructure:"system_prompt"`
	DefaultNegativePrompt string `mapstructure:"default_negative_prompt"`
}

type DatabaseConfig struct {
	Driver              string `mapstructure:"driver"`
	Host                string `mapstructure:"host"`
	Port                int    `mapstructure:"port"`
	User                string `mapstructure:"user"`
	Password            string `mapstructure:"password"`
	DBName              string `mapstructure:"dbname"`
	SSLMode             string `mapstructure:"sslmode"`
	MaxOpenConns        int    `mapstructure:"max_open_conns"`
	MaxIdleConns        int    `mapstructure:"max_idle_conns"`
	SQLitePath          string `mapstructure:"sqlite_path"`
	TablePrefix         string `mapstructure:"table_prefix"`
	AutoMigrateEnabled  bool   `mapstructure:"auto_migrate_enabled"`
	AllowStartupMigrate bool   `mapstructure:"allow_startup_migrate_in_non_dev"`
}

type RedisConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	MaxRetries   int           `mapstructure:"max_retries"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type SecurityConfig struct {
	JWTSecret        string `mapstructure:"jwt_secret"`
	EncryptionKey    string `mapstructure:"encryption_key"`
	ServiceSecretKey string `mapstructure:"service_secret_key"`
}

type PlatformConfig struct {
	BaseURL               string        `mapstructure:"base_url"`
	Timeout               time.Duration `mapstructure:"timeout"`
	ServiceName           string        `mapstructure:"service_name"`
	InternalServiceSecret string        `mapstructure:"internal_service_secret"`
	JWTSecret             string        `mapstructure:"jwt_secret"`
}

type MonitoringConfig struct {
	Metrics MetricsConfig `mapstructure:"metrics"`
	Tracing TracingConfig `mapstructure:"tracing"`
}

type MetricsConfig struct {
	Enabled          bool      `mapstructure:"enabled"`
	Port             int       `mapstructure:"port"`
	Path             string    `mapstructure:"path"`
	Namespace        string    `mapstructure:"namespace"`
	Subsystem        string    `mapstructure:"subsystem"`
	PushInterval     string    `mapstructure:"push_interval"`
	HistogramBuckets []float64 `mapstructure:"histogram_buckets"`
}

type TracingConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	ServiceName    string  `mapstructure:"service_name"`
	ServiceVersion string  `mapstructure:"service_version"`
	Environment    string  `mapstructure:"environment"`
	JaegerEndpoint string  `mapstructure:"jaeger_endpoint"`
	SampleRate     float64 `mapstructure:"sample_rate"`
	LogSpans       bool    `mapstructure:"log_spans"`
}

func Load(configFile string) (*Config, error) {
	if configFile == "" {
		configFile = "config.local"
	}
	v := viper.New()
	v.SetConfigName(strings.TrimSuffix(configFile, ".yaml"))
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/ecommerce-service/")
	v.SetEnvPrefix("ECOMMERCE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("host", "0.0.0.0")
	v.SetDefault("port", 8296)
	v.SetDefault("gin_mode", "debug")
	v.SetDefault("log_level", "info")
	v.SetDefault("use_mock", false)
	v.SetDefault("app.frontend_base_url", "http://localhost:5180")
	v.SetDefault("app.product_name", "Agent Ecommerce")
	v.SetDefault("app.product_code", "ecommerce")
	v.SetDefault("app.default_language", "zh")
	v.SetDefault("app.image_runtime.global_negative_prompt", "blurry, noise, jpeg artifacts, watermark, text overlay, extra limbs, missing limbs, deformed anatomy, disfigured, bad proportions, duplicate objects, floating objects with no shadow, unrealistic lighting inconsistency, oversaturated colors, artificial plastic texture, lowres, draft quality, sketch, illustration style")
	v.SetDefault("app.image_runtime.scene_prompt_policies", map[string]any{})
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.sqlite_path", "data/ecommerce.db")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "ecommerce")
	v.SetDefault("database.password", "ecommercepassword")
	v.SetDefault("database.dbname", "ecommerce")
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.table_prefix", "ecommerce_")
	v.SetDefault("database.auto_migrate_enabled", false)
	v.SetDefault("database.allow_startup_migrate_in_non_dev", false)
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 2)
	v.SetDefault("redis.pool_size", 10)
	v.SetDefault("redis.min_idle_conns", 2)
	v.SetDefault("redis.max_retries", 3)
	v.SetDefault("redis.dial_timeout", "5s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.write_timeout", "3s")
	v.SetDefault("security.jwt_secret", "ecommerce-dev-secret")
	v.SetDefault("security.encryption_key", "ecommerce-encryption-key-change-me")
	v.SetDefault("security.service_secret_key", "ecommerce-service-secret")
	v.SetDefault("platform.base_url", "http://localhost:8195")
	v.SetDefault("platform.timeout", "5s")
	v.SetDefault("platform.service_name", "v-ecommerce-backend")
	v.SetDefault("platform.internal_service_secret", "platform-internal-secret")
	v.SetDefault("platform.jwt_secret", "platform-dev-secret")
	v.SetDefault("monitoring.metrics.enabled", true)
	v.SetDefault("monitoring.metrics.port", 9096)
	v.SetDefault("monitoring.metrics.path", "/metrics")
	v.SetDefault("monitoring.metrics.namespace", "ecommerce")
	v.SetDefault("monitoring.metrics.subsystem", "service")
	v.SetDefault("monitoring.metrics.push_interval", "30s")
	v.SetDefault("monitoring.metrics.histogram_buckets", []float64{0.1, 0.5, 1, 2, 5, 10})
	v.SetDefault("monitoring.tracing.enabled", false)
	v.SetDefault("monitoring.tracing.service_name", "ecommerce-service")
	v.SetDefault("monitoring.tracing.service_version", "1.0.0")
	v.SetDefault("monitoring.tracing.environment", "development")
	v.SetDefault("monitoring.tracing.jaeger_endpoint", "http://localhost:14268/api/traces")
	v.SetDefault("monitoring.tracing.sample_rate", 1.0)
	v.SetDefault("monitoring.tracing.log_spans", false)
}
