package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set required environment variables for testing
	os.Setenv("STORAGE_ENDPOINT", "localhost:9000")
	os.Setenv("STORAGE_ACCESS_KEY", "test-access-key")
	os.Setenv("STORAGE_SECRET_KEY", "test-secret-key")
	os.Setenv("AUTH_ENABLED", "false") // Disable auth for testing

	defer func() {
		// Clean up environment variables
		os.Unsetenv("STORAGE_ENDPOINT")
		os.Unsetenv("STORAGE_ACCESS_KEY")
		os.Unsetenv("STORAGE_SECRET_KEY")
		os.Unsetenv("AUTH_ENABLED")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Test default values
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected server port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Storage.Bucket != "insights-ros-data" {
		t.Errorf("Expected storage bucket 'insights-ros-data', got %s", cfg.Storage.Bucket)
	}

	if cfg.Kafka.Topic != "hccm.ros.events" {
		t.Errorf("Expected Kafka topic 'hccm.ros.events', got %s", cfg.Kafka.Topic)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &Config{
				Storage: StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
				Auth: AuthConfig{
					Enabled:   false,
					JWTSecret: "",
				},
			},
			expectError: false,
		},
		{
			name: "missing storage endpoint",
			config: &Config{
				Storage: StorageConfig{
					Endpoint:  "",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
			},
			expectError: true,
		},
		{
			name: "missing kafka brokers",
			config: &Config{
				Storage: StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: KafkaConfig{
					Brokers: []string{},
					Topic:   "test-topic",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError && err == nil {
				t.Errorf("Expected validation error, but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestIsClowderEnabled(t *testing.T) {
	cfg := &Config{}
	if cfg.IsClowderEnabled() {
		t.Error("Expected Clowder to be disabled")
	}
}