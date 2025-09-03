package upload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestPayloadExtractor_ExtractPayload(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create a test logger
	logger := logrus.New()
	logger.SetOutput(os.Stdout)

	// Create payload extractor
	extractor := NewPayloadExtractor(tempDir, logger)

	// Create test manifest
	manifest := &Manifest{
		UUID:                      "test-uuid-123",
		ClusterID:                 "test-cluster-456",
		ClusterAlias:              "test-cluster",
		Date:                      time.Now(),
		Files:                     []string{"usage.csv"},
		ResourceOptimizationFiles: []string{"ros-data.csv"},
		Certified:                 true,
		OperatorVersion:           "1.0.0",
	}

	// Create test tar.gz payload
	payload, err := createTestPayload(manifest)
	if err != nil {
		t.Fatalf("Failed to create test payload: %v", err)
	}

	// Extract payload
	result, err := extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
	if err != nil {
		t.Fatalf("Failed to extract payload: %v", err)
	}
	defer result.Cleanup()

	// Verify results
	if result.Manifest.UUID != manifest.UUID {
		t.Errorf("Expected manifest UUID %s, got %s", manifest.UUID, result.Manifest.UUID)
	}

	if result.Manifest.ClusterID != manifest.ClusterID {
		t.Errorf("Expected cluster ID %s, got %s", manifest.ClusterID, result.Manifest.ClusterID)
	}

	if len(result.ROSFiles) != 1 {
		t.Errorf("Expected 1 ROS file, got %d", len(result.ROSFiles))
	}

	if _, exists := result.ROSFiles["ros-data.csv"]; !exists {
		t.Error("Expected ros-data.csv to be in ROS files")
	}
}

func TestPayloadExtractor_ExtractPayload_NoManifest(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	extractor := NewPayloadExtractor(tempDir, logger)

	// Create payload without manifest
	payload, err := createTestPayloadWithoutManifest()
	if err != nil {
		t.Fatalf("Failed to create test payload: %v", err)
	}

	// Extract payload should fail
	_, err = extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
	if err == nil {
		t.Error("Expected error when manifest is missing, but got none")
	}

	if !strings.Contains(err.Error(), "manifest.json not found") {
		t.Errorf("Expected manifest not found error, got: %v", err)
	}
}

func TestPayloadExtractor_ExtractPayload_NoROSFiles(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	extractor := NewPayloadExtractor(tempDir, logger)

	// Create manifest without ROS files
	manifest := &Manifest{
		UUID:                      "test-uuid-123",
		ClusterID:                 "test-cluster-456",
		Date:                      time.Now(),
		Files:                     []string{"usage.csv"},
		ResourceOptimizationFiles: []string{}, // No ROS files
	}

	payload, err := createTestPayload(manifest)
	if err != nil {
		t.Fatalf("Failed to create test payload: %v", err)
	}

	// Extract payload should fail
	_, err = extractor.ExtractPayload(bytes.NewReader(payload), "test-request-123")
	if err == nil {
		t.Error("Expected error when no ROS files, but got none")
	}

	if !strings.Contains(err.Error(), "no ROS files") {
		t.Errorf("Expected no ROS files error, got: %v", err)
	}
}

// Helper function to create a test tar.gz payload
func createTestPayload(manifest *Manifest) ([]byte, error) {
	var buf bytes.Buffer

	// Create gzip writer
	gzipWriter := gzip.NewWriter(&buf)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add manifest.json
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	manifestHeader := &tar.Header{
		Name:     "manifest.json",
		Mode:     0644,
		Size:     int64(len(manifestJSON)),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(manifestHeader); err != nil {
		return nil, err
	}

	if _, err := tarWriter.Write(manifestJSON); err != nil {
		return nil, err
	}

	// Add usage.csv file
	usageData := []byte("timestamp,cpu,memory\n2023-01-01T00:00:00Z,0.5,1024\n")
	usageHeader := &tar.Header{
		Name:     "usage.csv",
		Mode:     0644,
		Size:     int64(len(usageData)),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(usageHeader); err != nil {
		return nil, err
	}

	if _, err := tarWriter.Write(usageData); err != nil {
		return nil, err
	}

	// Add ROS file if specified in manifest
	if len(manifest.ResourceOptimizationFiles) > 0 {
		rosData := []byte("node,cpu_request,memory_request\nnode1,100m,256Mi\n")
		rosHeader := &tar.Header{
			Name:     manifest.ResourceOptimizationFiles[0],
			Mode:     0644,
			Size:     int64(len(rosData)),
			Typeflag: tar.TypeReg,
		}

		if err := tarWriter.WriteHeader(rosHeader); err != nil {
			return nil, err
		}

		if _, err := tarWriter.Write(rosData); err != nil {
			return nil, err
		}
	}

	// Close writers to flush data
	tarWriter.Close()
	gzipWriter.Close()

	return buf.Bytes(), nil
}

// Helper function to create a test payload without manifest
func createTestPayloadWithoutManifest() ([]byte, error) {
	var buf bytes.Buffer

	gzipWriter := gzip.NewWriter(&buf)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add only a regular file, no manifest
	data := []byte("test data")
	header := &tar.Header{
		Name:     "test.txt",
		Mode:     0644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return nil, err
	}

	if _, err := tarWriter.Write(data); err != nil {
		return nil, err
	}

	tarWriter.Close()
	gzipWriter.Close()

	return buf.Bytes(), nil
}