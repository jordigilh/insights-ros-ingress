package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	"github.com/RedHatInsights/insights-ros-ingress/internal/health"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/sirupsen/logrus"
)

// Producer wraps Kafka producer with additional functionality
type Producer struct {
	producer *kafka.Producer
	config   config.KafkaConfig
	logger   *logrus.Logger
}

// ROSMessage represents a ROS event message
// Matches the structure used by koku's ROSReportShipper
type ROSMessage struct {
	RequestID   string            `json:"request_id"`
	B64Identity string            `json:"b64_identity"`
	Metadata    ROSMetadata       `json:"metadata"`
	Files       []string          `json:"files"`
	ObjectKeys  []string          `json:"object_keys"`
}

// ROSMetadata represents metadata for ROS events
type ROSMetadata struct {
	Account         string `json:"account"`
	OrgID           string `json:"org_id"`
	SourceID        string `json:"source_id"`
	ProviderUUID    string `json:"provider_uuid"`
	ClusterUUID     string `json:"cluster_uuid"`
	ClusterAlias    string `json:"cluster_alias"`
	OperatorVersion string `json:"operator_version"`
}

// ValidationMessage represents a validation message for upload service
type ValidationMessage struct {
	RequestID  string `json:"request_id"`
	Validation string `json:"validation"`
}

// NewKafkaProducer creates a new Kafka producer
func NewKafkaProducer(cfg config.KafkaConfig) (*Producer, error) {
	// Configure Kafka producer
	kafkaConfig := kafka.ConfigMap{
		"bootstrap.servers": fmt.Sprintf("%v", cfg.Brokers),
		"client.id":         cfg.ClientID,
		"acks":              "all",
		"retries":           cfg.Retries,
		"batch.size":        cfg.BatchSize,
		"linger.ms":         5,
		"compression.type":  "snappy",
		"idempotent":        true,
	}

	// Add security configuration if specified
	if cfg.SecurityProtocol != "PLAINTEXT" {
		kafkaConfig["security.protocol"] = cfg.SecurityProtocol

		if cfg.SASLMechanism != "" {
			kafkaConfig["sasl.mechanism"] = cfg.SASLMechanism
			kafkaConfig["sasl.username"] = cfg.SASLUsername
			kafkaConfig["sasl.password"] = cfg.SASLPassword
		}

		if cfg.SSLCALocation != "" {
			kafkaConfig["ssl.ca.location"] = cfg.SSLCALocation
		}
	}

	// Create producer
	producer, err := kafka.NewProducer(&kafkaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	p := &Producer{
		producer: producer,
		config:   cfg,
		logger:   logrus.New(),
	}

	// Start delivery report handler
	go p.handleDeliveryReports()

	return p, nil
}

// SendROSEvent sends a ROS event message to Kafka
func (p *Producer) SendROSEvent(ctx context.Context, msg *ROSMessage) error {
	start := time.Now()
	defer func() {
		health.KafkaMessageDuration.WithLabelValues(p.config.Topic).Observe(time.Since(start).Seconds())
	}()

	// Marshal message to JSON
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "marshal_error").Inc()
		return fmt.Errorf("failed to marshal ROS message: %w", err)
	}

	// Create Kafka message
	kafkaMsg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &p.config.Topic,
			Partition: kafka.PartitionAny,
		},
		Key:   []byte(msg.RequestID),
		Value: msgBytes,
		Headers: []kafka.Header{
			{Key: "service", Value: []byte("ros")},
			{Key: "request_id", Value: []byte(msg.RequestID)},
			{Key: "org_id", Value: []byte(msg.Metadata.OrgID)},
		},
	}

	// Send message
	deliveryChan := make(chan kafka.Event)
	err = p.producer.Produce(kafkaMsg, deliveryChan)
	if err != nil {
		health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "produce_error").Inc()
		close(deliveryChan)
		return fmt.Errorf("failed to produce ROS message: %w", err)
	}

	// Wait for delivery confirmation
	select {
	case e := <-deliveryChan:
		close(deliveryChan)
		if m, ok := e.(*kafka.Message); ok {
			if m.TopicPartition.Error != nil {
				health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "delivery_error").Inc()
				return fmt.Errorf("message delivery failed: %w", m.TopicPartition.Error)
			}
			health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "success").Inc()
			p.logger.WithFields(logrus.Fields{
				"topic":      *m.TopicPartition.Topic,
				"partition":  m.TopicPartition.Partition,
				"offset":     m.TopicPartition.Offset,
				"request_id": msg.RequestID,
			}).Debug("ROS message delivered successfully")
		}
	case <-ctx.Done():
		close(deliveryChan)
		health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "timeout").Inc()
		return fmt.Errorf("message delivery timeout: %w", ctx.Err())
	case <-time.After(30 * time.Second):
		close(deliveryChan)
		health.KafkaMessagesTotal.WithLabelValues(p.config.Topic, "timeout").Inc()
		return fmt.Errorf("message delivery timeout after 30 seconds")
	}

	return nil
}

// SendValidationMessage sends a validation message to the upload service
func (p *Producer) SendValidationMessage(ctx context.Context, requestID, status string) error {
	validationTopic := "platform.upload.validation"
	if p.config.SecurityProtocol != "" {
		// Topic might be configured differently in different environments
		validationTopic = "platform.upload.validation"
	}

	start := time.Now()
	defer func() {
		health.KafkaMessageDuration.WithLabelValues(validationTopic).Observe(time.Since(start).Seconds())
	}()

	msg := &ValidationMessage{
		RequestID:  requestID,
		Validation: status,
	}

	// Marshal message to JSON
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		health.KafkaMessagesTotal.WithLabelValues(validationTopic, "marshal_error").Inc()
		return fmt.Errorf("failed to marshal validation message: %w", err)
	}

	// Create Kafka message
	kafkaMsg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &validationTopic,
			Partition: kafka.PartitionAny,
		},
		Key:   []byte(requestID),
		Value: msgBytes,
		Headers: []kafka.Header{
			{Key: "service", Value: []byte("ingress")},
			{Key: "request_id", Value: []byte(requestID)},
		},
	}

	// Send message
	deliveryChan := make(chan kafka.Event)
	err = p.producer.Produce(kafkaMsg, deliveryChan)
	if err != nil {
		health.KafkaMessagesTotal.WithLabelValues(validationTopic, "produce_error").Inc()
		close(deliveryChan)
		return fmt.Errorf("failed to produce validation message: %w", err)
	}

	// Wait for delivery confirmation
	select {
	case e := <-deliveryChan:
		close(deliveryChan)
		if m, ok := e.(*kafka.Message); ok {
			if m.TopicPartition.Error != nil {
				health.KafkaMessagesTotal.WithLabelValues(validationTopic, "delivery_error").Inc()
				return fmt.Errorf("validation message delivery failed: %w", m.TopicPartition.Error)
			}
			health.KafkaMessagesTotal.WithLabelValues(validationTopic, "success").Inc()
			p.logger.WithFields(logrus.Fields{
				"topic":      *m.TopicPartition.Topic,
				"partition":  m.TopicPartition.Partition,
				"offset":     m.TopicPartition.Offset,
				"request_id": requestID,
				"status":     status,
			}).Debug("Validation message delivered successfully")
		}
	case <-ctx.Done():
		close(deliveryChan)
		health.KafkaMessagesTotal.WithLabelValues(validationTopic, "timeout").Inc()
		return fmt.Errorf("validation message delivery timeout: %w", ctx.Err())
	case <-time.After(10 * time.Second):
		close(deliveryChan)
		health.KafkaMessagesTotal.WithLabelValues(validationTopic, "timeout").Inc()
		return fmt.Errorf("validation message delivery timeout after 10 seconds")
	}

	return nil
}

// handleDeliveryReports handles delivery reports in the background
func (p *Producer) handleDeliveryReports() {
	for e := range p.producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				p.logger.WithError(ev.TopicPartition.Error).Error("Message delivery failed")
			} else {
				p.logger.WithFields(logrus.Fields{
					"topic":     *ev.TopicPartition.Topic,
					"partition": ev.TopicPartition.Partition,
					"offset":    ev.TopicPartition.Offset,
				}).Debug("Message delivered")
			}
		case kafka.Error:
			p.logger.WithError(ev).Error("Kafka error")
		default:
			p.logger.WithField("event", ev).Debug("Ignored Kafka event")
		}
	}
}

// HealthCheck performs a health check on the Kafka connection
func (p *Producer) HealthCheck() error {
	// Get metadata to verify connection
	metadata, err := p.producer.GetMetadata(nil, false, 5000)
	if err != nil {
		return fmt.Errorf("Kafka health check failed: %w", err)
	}

	// Check if we can see any brokers
	if len(metadata.Brokers) == 0 {
		return fmt.Errorf("no Kafka brokers available")
	}

	// Check if our topic exists
	for _, topic := range metadata.Topics {
		if topic.Topic == p.config.Topic {
			// Topic exists and is accessible
			return nil
		}
	}

	// Topic doesn't exist, but connection is working
	p.logger.WithField("topic", p.config.Topic).Warn("ROS topic not found, but Kafka connection is healthy")
	return nil
}

// Flush flushes any outstanding messages
func (p *Producer) Flush(timeout time.Duration) error {
	remaining := p.producer.Flush(int(timeout.Milliseconds()))
	if remaining > 0 {
		return fmt.Errorf("failed to flush %d messages within timeout", remaining)
	}
	return nil
}

// Close closes the Kafka producer
func (p *Producer) Close() error {
	// Flush remaining messages
	p.producer.Flush(5000) // 5 second timeout

	// Close producer
	p.producer.Close()
	return nil
}