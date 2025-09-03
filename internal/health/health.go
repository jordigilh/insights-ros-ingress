package health

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Version     string            `json:"version"`
	Checks      map[string]Check  `json:"checks"`
}

// Check represents an individual health check
type Check struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Latency time.Duration `json:"latency,omitempty"`
}

// Checker provides health check functionality
type Checker struct {
	storageClient   StorageChecker
	messagingClient MessagingChecker
	version         string
}

// StorageChecker interface for storage health checks
type StorageChecker interface {
	HealthCheck() error
}

// MessagingChecker interface for messaging health checks
type MessagingChecker interface {
	HealthCheck() error
}

// NewChecker creates a new health checker
func NewChecker(storageClient StorageChecker, messagingClient MessagingChecker) *Checker {
	return &Checker{
		storageClient:   storageClient,
		messagingClient: messagingClient,
		version:         "1.0.0",
	}
}

// Health handles the health check endpoint
func (c *Checker) Health(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]Check)
	overallStatus := "healthy"

	// Check storage connectivity
	start := time.Now()
	if err := c.storageClient.HealthCheck(); err != nil {
		checks["storage"] = Check{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: time.Since(start),
		}
		overallStatus = "unhealthy"
	} else {
		checks["storage"] = Check{
			Status:  "healthy",
			Latency: time.Since(start),
		}
	}

	// Check messaging connectivity
	start = time.Now()
	if err := c.messagingClient.HealthCheck(); err != nil {
		checks["messaging"] = Check{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: time.Since(start),
		}
		overallStatus = "unhealthy"
	} else {
		checks["messaging"] = Check{
			Status:  "healthy",
			Latency: time.Since(start),
		}
	}

	response := HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Version:   c.version,
		Checks:    checks,
	}

	w.Header().Set("Content-Type", "application/json")

	// Set appropriate HTTP status code
	if overallStatus == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(response)
}

// Ready handles the readiness probe endpoint
func (c *Checker) Ready(w http.ResponseWriter, r *http.Request) {
	// For readiness, we just check if the service can start
	// More basic than health check
	response := map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now(),
		"version":   c.version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// Metrics handles the metrics endpoint
func (c *Checker) Metrics(w http.ResponseWriter, r *http.Request) {
	// Serve Prometheus metrics
	promhttp.Handler().ServeHTTP(w, r)
}

// Prometheus metrics
var (
	// HTTP request metrics
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// Upload metrics
	UploadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "uploads_total",
			Help: "Total number of uploads processed",
		},
		[]string{"status", "content_type"},
	)

	UploadSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "upload_size_bytes",
			Help:    "Size of uploaded files in bytes",
			Buckets: []float64{1024, 10240, 102400, 1048576, 10485760, 104857600, 1073741824},
		},
		[]string{"content_type"},
	)

	// Storage metrics
	StorageOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "storage_operations_total",
			Help: "Total number of storage operations",
		},
		[]string{"operation", "status"},
	)

	StorageOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "storage_operation_duration_seconds",
			Help:    "Duration of storage operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Kafka metrics
	KafkaMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_messages_total",
			Help: "Total number of Kafka messages sent",
		},
		[]string{"topic", "status"},
	)

	KafkaMessageDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_message_duration_seconds",
			Help:    "Duration of Kafka message operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"topic"},
	)
)

// InitMetrics initializes Prometheus metrics
func InitMetrics() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		UploadsTotal,
		UploadSizeBytes,
		StorageOperationsTotal,
		StorageOperationDuration,
		KafkaMessagesTotal,
		KafkaMessageDuration,
	)
}