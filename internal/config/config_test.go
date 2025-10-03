package config_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
)

var _ = Describe("Configuration Loading", func() {
	Context("When environment variables are set", func() {
		BeforeEach(func() {
			// Set required environment variables for testing
			Expect(os.Setenv("STORAGE_ENDPOINT", "localhost:9000")).To(Succeed())
			Expect(os.Setenv("STORAGE_ACCESS_KEY", "test-access-key")).To(Succeed())
			Expect(os.Setenv("STORAGE_SECRET_KEY", "test-secret-key")).To(Succeed())
			Expect(os.Setenv("AUTH_ENABLED", "false")).To(Succeed()) // Disable auth for testing
		})

		AfterEach(func() {
			// Clean up environment variables
			Expect(os.Unsetenv("STORAGE_ENDPOINT")).To(Succeed())
			Expect(os.Unsetenv("STORAGE_ACCESS_KEY")).To(Succeed())
			Expect(os.Unsetenv("STORAGE_SECRET_KEY")).To(Succeed())
			Expect(os.Unsetenv("AUTH_ENABLED")).To(Succeed())
		})

		It("should load configuration successfully", func() {
			cfg, err := config.Load()
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg).ToNot(BeNil())
		})

		It("should set correct default values", func() {
			cfg, err := config.Load()
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Server.Port).To(Equal(8080))
			Expect(cfg.Storage.Bucket).To(Equal("insights-ros-data"))
			Expect(cfg.Kafka.Topic).To(Equal("hccm.ros.events"))
		})

		It("should use environment variables when provided", func() {
			cfg, err := config.Load()
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Storage.Endpoint).To(Equal("localhost:9000"))
			Expect(cfg.Storage.AccessKey).To(Equal("test-access-key"))
			Expect(cfg.Storage.SecretKey).To(Equal("test-secret-key"))
			Expect(cfg.Auth.Enabled).To(BeFalse())
		})
	})
})

var _ = Describe("Configuration Validation", func() {
	Context("With valid configuration", func() {
		It("should pass validation", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
				Auth: config.AuthConfig{
					Enabled: false,
				},
			}

			err := cfg.Validate()
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("With missing storage endpoint", func() {
		It("should return validation error", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
			}

			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("storage endpoint is required"))
		})
	})

	Context("With missing storage credentials", func() {
		It("should return validation error when access key is missing", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "",
					SecretKey: "test-secret",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
			}

			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("storage credentials are required"))
		})

		It("should return validation error when secret key is missing", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test-topic",
				},
			}

			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("storage credentials are required"))
		})
	})

	Context("With missing kafka brokers", func() {
		It("should return validation error", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{},
					Topic:   "test-topic",
				},
			}

			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kafka brokers are required"))
		})
	})

	Context("With missing kafka topic", func() {
		It("should return validation error", func() {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Endpoint:  "localhost:9000",
					AccessKey: "test-key",
					SecretKey: "test-secret",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "",
				},
			}

			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kafka topic is required"))
		})
	})

})

var _ = Describe("Clowder Configuration", func() {
	It("should return false when Clowder is not enabled", func() {
		cfg := &config.Config{}
		Expect(cfg.IsClowderEnabled()).To(BeFalse())
	})
})
