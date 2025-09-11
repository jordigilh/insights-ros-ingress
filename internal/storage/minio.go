package storage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	"github.com/RedHatInsights/insights-ros-ingress/internal/health"
	"github.com/minio/minio-go/v6"
	"github.com/sirupsen/logrus"
)

// Client wraps MinIO client with additional functionality
type Client struct {
	client *minio.Client
	config config.StorageConfig
	logger *logrus.Logger
}

// UploadRequest represents a file upload request
type UploadRequest struct {
	Key         string
	Data        io.Reader
	Size        int64
	ContentType string
	Metadata    map[string]string
}

// UploadResult represents the result of a file upload
type UploadResult struct {
	Key           string
	URL           string
	PresignedURL  string
	Size          int64
	ETag          string
}

// NewMinIOClient creates a new MinIO client
func NewMinIOClient(cfg config.StorageConfig) (*Client, error) {
	// Initialize MinIO client
	minioClient, err := minio.New(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.UseSSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	client := &Client{
		client: minioClient,
		config: cfg,
		logger: logrus.New(),
	}

	// Ensure bucket exists

	exists, err := minioClient.BucketExists(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = minioClient.MakeBucket(cfg.Bucket, cfg.Region)
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
		client.logger.WithField("bucket", cfg.Bucket).Info("Created MinIO bucket")
	}

	return client, nil
}

// Upload uploads a file to MinIO storage
func (c *Client) Upload(ctx context.Context, req *UploadRequest) (*UploadResult, error) {
	start := time.Now()
	defer func() {
		health.StorageOperationDuration.WithLabelValues("upload").Observe(time.Since(start).Seconds())
	}()

	// Add path prefix if configured
	key := req.Key
	if c.config.PathPrefix != "" {
		key = filepath.Join(c.config.PathPrefix, key)
	}

	// Prepare upload options
	opts := minio.PutObjectOptions{
		ContentType:  req.ContentType,
		UserMetadata: req.Metadata,
	}

	// Upload to MinIO
	n, err := c.client.PutObject(c.config.Bucket, key, req.Data, req.Size, opts)
	if err != nil {
		health.StorageOperationsTotal.WithLabelValues("upload", "error").Inc()
		return nil, fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	health.StorageOperationsTotal.WithLabelValues("upload", "success").Inc()

	// Generate presigned URL for access
	presignedURL, err := c.GeneratePresignedURL(ctx, key)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to generate presigned URL")
	}

	result := &UploadResult{
		Key:          key,
		URL:          fmt.Sprintf("%s/%s/%s", c.getEndpointURL(), c.config.Bucket, key),
		PresignedURL: presignedURL,
		Size:         n,
		ETag:         "", // ETag not available in v6 PutObject response
	}

	c.logger.WithFields(logrus.Fields{
		"key":  key,
		"size": n,
	}).Debug("Successfully uploaded file to MinIO")

	return result, nil
}

// GeneratePresignedURL generates a presigned URL for file access
func (c *Client) GeneratePresignedURL(ctx context.Context, key string) (string, error) {
	start := time.Now()
	defer func() {
		health.StorageOperationDuration.WithLabelValues("presign").Observe(time.Since(start).Seconds())
	}()

	expiry := time.Duration(c.config.URLExpiration) * time.Second
	url, err := c.client.PresignedGetObject(c.config.Bucket, key, expiry, nil)
	if err != nil {
		health.StorageOperationsTotal.WithLabelValues("presign", "error").Inc()
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	health.StorageOperationsTotal.WithLabelValues("presign", "success").Inc()
	return url.String(), nil
}

// Delete removes a file from MinIO storage
func (c *Client) Delete(ctx context.Context, key string) error {
	start := time.Now()
	defer func() {
		health.StorageOperationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	}()

	// Add path prefix if configured
	if c.config.PathPrefix != "" {
		key = filepath.Join(c.config.PathPrefix, key)
	}

	err := c.client.RemoveObject(c.config.Bucket, key)
	if err != nil {
		health.StorageOperationsTotal.WithLabelValues("delete", "error").Inc()
		return fmt.Errorf("failed to delete from MinIO: %w", err)
	}

	health.StorageOperationsTotal.WithLabelValues("delete", "success").Inc()

	c.logger.WithField("key", key).Debug("Successfully deleted file from MinIO")
	return nil
}

// List lists objects in the bucket with a given prefix
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	start := time.Now()
	defer func() {
		health.StorageOperationDuration.WithLabelValues("list").Observe(time.Since(start).Seconds())
	}()

	// Add path prefix if configured
	if c.config.PathPrefix != "" {
		prefix = filepath.Join(c.config.PathPrefix, prefix)
	}

	var objects []string
	doneCh := make(chan struct{})
	defer close(doneCh)
	objectCh := c.client.ListObjects(c.config.Bucket, prefix, true, doneCh)

	for object := range objectCh {
		if object.Err != nil {
			health.StorageOperationsTotal.WithLabelValues("list", "error").Inc()
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}
		objects = append(objects, object.Key)
	}

	health.StorageOperationsTotal.WithLabelValues("list", "success").Inc()
	return objects, nil
}

// HealthCheck performs a health check on the storage connection
func (c *Client) HealthCheck() error {
	// Try to list buckets to verify connectivity
	_, err := c.client.ListBuckets()
	if err != nil {
		return fmt.Errorf("MinIO health check failed: %w", err)
	}

	// Verify our bucket exists
	exists, err := c.client.BucketExists(c.config.Bucket)
	if err != nil {
		return fmt.Errorf("MinIO bucket check failed: %w", err)
	}

	if !exists {
		return fmt.Errorf("MinIO bucket '%s' does not exist", c.config.Bucket)
	}

	return nil
}

// GenerateUploadPath generates a standardized upload path
func (c *Client) GenerateUploadPath(schema, sourceID, date, filename string) string {
	return filepath.Join(schema, fmt.Sprintf("source=%s", sourceID), fmt.Sprintf("date=%s", date), filename)
}

// getEndpointURL returns the full endpoint URL for MinIO
func (c *Client) getEndpointURL() string {
	scheme := "http"
	if c.config.UseSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.config.Endpoint)
}

// Close closes the MinIO client (MinIO client doesn't require explicit closing)
func (c *Client) Close() error {
	// MinIO client doesn't require explicit closing
	return nil
}