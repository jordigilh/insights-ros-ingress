package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config represents the application configuration
// Designed to mimic Clowder behavior but work independently in K8s
type Config struct {
	Server  ServerConfig  `json:"server"`
	Storage StorageConfig `json:"storage"`
	Kafka   KafkaConfig   `json:"kafka"`
	Upload  UploadConfig  `json:"upload"`
	Logging LoggingConfig `json:"logging"`
	Metrics MetricsConfig `json:"metrics"`
	Auth    AuthConfig    `json:"auth"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port         int  `json:"port"`
	ReadTimeout  int  `json:"readTimeout"`
	WriteTimeout int  `json:"writeTimeout"`
	IdleTimeout  int  `json:"idleTimeout"`
	Debug        bool `json:"debug"`
}

// StorageConfig holds MinIO/S3 storage configuration
type StorageConfig struct {
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	Bucket        string `json:"bucket"`
	AccessKey     string `json:"accessKey"`
	SecretKey     string `json:"secretKey"`
	UseSSL        bool   `json:"useSSL"`
	URLExpiration int    `json:"urlExpiration"`
	PathPrefix    string `json:"pathPrefix"`
}

// KafkaConfig holds Kafka configuration
type KafkaConfig struct {
	Brokers          []string `json:"brokers"`
	Topic            string   `json:"topic"`
	SecurityProtocol string   `json:"securityProtocol"`
	SASLMechanism    string   `json:"saslMechanism"`
	SASLUsername     string   `json:"saslUsername"`
	SASLPassword     string   `json:"saslPassword"`
	SSLCALocation    string   `json:"sslCaLocation"`
	ClientID         string   `json:"clientId"`
	BatchSize        int      `json:"batchSize"`
	Retries          int      `json:"retries"`
}

// UploadConfig holds upload processing configuration
type UploadConfig struct {
	MaxUploadSize   int64    `json:"maxUploadSize"`
	MaxMemory       int64    `json:"maxMemory"`
	TempDir         string   `json:"tempDir"`
	AllowedTypes    []string `json:"allowedTypes"`
	RequireAuth     bool     `json:"requireAuth"`
	ValidationTopic string   `json:"validationTopic"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
	Port    int    `json:"port"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled     bool     `json:"enabled"`
	AllowedOrgs []string `json:"allowedOrgs"`
}

// Load reads configuration from environment variables and files
// Following Clowder patterns for K8s deployment compatibility
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:         getEnvInt("SERVER_PORT", 8080),
			ReadTimeout:  getEnvInt("SERVER_READ_TIMEOUT", 30),
			WriteTimeout: getEnvInt("SERVER_WRITE_TIMEOUT", 30),
			IdleTimeout:  getEnvInt("SERVER_IDLE_TIMEOUT", 120),
			Debug:        getEnvBool("DEBUG", false),
		},
		Storage: StorageConfig{
			Endpoint:      getEnvString("STORAGE_ENDPOINT", ""),
			Region:        getEnvString("STORAGE_REGION", "us-east-1"),
			Bucket:        getEnvString("STORAGE_BUCKET", "insights-ros-data"),
			AccessKey:     getEnvString("STORAGE_ACCESS_KEY", ""),
			SecretKey:     getEnvString("STORAGE_SECRET_KEY", ""),
			UseSSL:        getEnvBool("STORAGE_USE_SSL", false),
			URLExpiration: getEnvInt("STORAGE_URL_EXPIRATION", 172800), // 48 hours
			PathPrefix:    getEnvString("STORAGE_PATH_PREFIX", "ros"),
		},
		Kafka: KafkaConfig{
			Brokers:          getEnvStringSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			Topic:            getEnvString("KAFKA_ROS_TOPIC", "hccm.ros.events"),
			SecurityProtocol: getEnvString("KAFKA_SECURITY_PROTOCOL", "PLAINTEXT"),
			SASLMechanism:    getEnvString("KAFKA_SASL_MECHANISM", ""),
			SASLUsername:     getEnvString("KAFKA_SASL_USERNAME", ""),
			SASLPassword:     getEnvString("KAFKA_SASL_PASSWORD", ""),
			SSLCALocation:    getEnvString("KAFKA_SSL_CA_LOCATION", ""),
			ClientID:         getEnvString("KAFKA_CLIENT_ID", "insights-ros-ingress"),
			BatchSize:        getEnvInt("KAFKA_BATCH_SIZE", 16384),
			Retries:          getEnvInt("KAFKA_RETRIES", 3),
		},
		Upload: UploadConfig{
			MaxUploadSize: getEnvInt64("UPLOAD_MAX_SIZE", 100*1024*1024),  // 100MB
			MaxMemory:     getEnvInt64("UPLOAD_MAX_MEMORY", 32*1024*1024), // 32MB
			TempDir:       getEnvString("UPLOAD_TEMP_DIR", "/tmp"),
			AllowedTypes:  getEnvStringSlice("UPLOAD_ALLOWED_TYPES", []string{"application/vnd.redhat.hccm.upload"}),
			RequireAuth:   getEnvBool("UPLOAD_REQUIRE_AUTH", true),

			// TODO: Remove the validation topic from the config
			ValidationTopic: getEnvString("KAFKA_VALIDATION_TOPIC", "platform.upload.validation"),
		},
		Logging: LoggingConfig{
			Level:  getEnvString("LOG_LEVEL", "info"),
			Format: getEnvString("LOG_FORMAT", "json"),
			Output: getEnvString("LOG_OUTPUT", "stdout"),
		},
		Metrics: MetricsConfig{
			Enabled: getEnvBool("METRICS_ENABLED", true),
			Path:    getEnvString("METRICS_PATH", "/metrics"),
			Port:    getEnvInt("METRICS_PORT", 8080),
		},
		Auth: AuthConfig{
			Enabled:     getEnvBool("AUTH_ENABLED", true),
			AllowedOrgs: getEnvStringSlice("AUTH_ALLOWED_ORGS", []string{}),
		},
	}

	// Validate required configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Storage validation
	if c.Storage.Endpoint == "" {
		return fmt.Errorf("storage endpoint is required")
	}
	if c.Storage.AccessKey == "" || c.Storage.SecretKey == "" {
		return fmt.Errorf("storage credentials are required")
	}

	// Kafka validation
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("kafka brokers are required")
	}
	if c.Kafka.Topic == "" {
		return fmt.Errorf("kafka topic is required")
	}

	return nil
}

// IsClowderEnabled returns false as this service doesn't use Clowder
// Included for compatibility with existing Insights services
func (c *Config) IsClowderEnabled() bool {
	return false
}

// GetMetricsAddr returns the metrics server address
func (c *Config) GetMetricsAddr() string {
	return fmt.Sprintf(":%d", c.Metrics.Port)
}

// GetWebPort returns the web server port
func (c *Config) GetWebPort() int {
	return c.Server.Port
}

// Helper functions for environment variable parsing

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
