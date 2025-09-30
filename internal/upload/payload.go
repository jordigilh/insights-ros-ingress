package upload

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Manifest represents the manifest.json structure from OCP payload
// Based on koku's manifest parsing logic
type Manifest struct {
	UUID                      string                 `json:"uuid"`
	ClusterID                 string                 `json:"cluster_id"`
	ClusterAlias              string                 `json:"cluster_alias,omitempty"`
	Date                      time.Time              `json:"date"`
	Start                     *time.Time             `json:"start,omitempty"`
	End                       *time.Time             `json:"end,omitempty"`
	Files                     []string               `json:"files"`
	ResourceOptimizationFiles []string               `json:"resource_optimization_files,omitempty"`
	Certified                 bool                   `json:"certified,omitempty"`
	OperatorVersion           string                 `json:"operator_version,omitempty"`
	DailyReports              bool                   `json:"daily_reports,omitempty"`
	CRStatus                  map[string]interface{} `json:"cr_status,omitempty"`
}

// PayloadExtractor handles extraction and processing of tar.gz payloads
type PayloadExtractor struct {
	tempDir string
	logger  *logrus.Logger
}

// ExtractedPayload represents the extracted payload contents
type ExtractedPayload struct {
	Manifest  *Manifest
	ROSFiles  map[string]string // filename -> file path
	TempDir   string
	RequestID string
}

// NewPayloadExtractor creates a new payload extractor
func NewPayloadExtractor(tempDir string, logger *logrus.Logger) *PayloadExtractor {
	return &PayloadExtractor{
		tempDir: tempDir,
		logger:  logger,
	}
}

// ExtractPayload extracts and validates a tar.gz payload
func (pe *PayloadExtractor) ExtractPayload(payloadData io.Reader, requestID string) (*ExtractedPayload, error) {
	// Create temporary directory for extraction
	extractDir := filepath.Join(pe.tempDir, requestID)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create extraction directory: %w", err)
	}

	pe.logger.WithFields(logrus.Fields{
		"request_id":  requestID,
		"extract_dir": extractDir,
	}).Debug("Starting payload extraction")

	// Extract tar.gz content
	extractedFiles, err := pe.extractTarGz(payloadData, extractDir)
	if err != nil {
		pe.cleanup(extractDir)
		return nil, fmt.Errorf("failed to extract tar.gz: %w", err)
	}

	// Find and parse manifest.json
	manifest, err := pe.findAndParseManifest(extractedFiles, extractDir)
	if err != nil {
		pe.cleanup(extractDir)
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Identify ROS files
	rosFiles, err := pe.identifyROSFiles(manifest, extractedFiles, extractDir)
	if err != nil {
		pe.cleanup(extractDir)
		return nil, fmt.Errorf("failed to identify ROS files: %w", err)
	}

	pe.logger.WithFields(logrus.Fields{
		"request_id":      requestID,
		"manifest_uuid":   manifest.UUID,
		"cluster_id":      manifest.ClusterID,
		"ros_files_count": len(rosFiles),
	}).Info("Successfully extracted payload")

	return &ExtractedPayload{
		Manifest:  manifest,
		ROSFiles:  rosFiles,
		TempDir:   extractDir,
		RequestID: requestID,
	}, nil
}

// extractTarGz extracts a tar.gz archive to the specified directory
func (pe *PayloadExtractor) extractTarGz(data io.Reader, destDir string) ([]string, error) {
	// Create gzip reader
	gzReader, err := gzip.NewReader(data)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		if err := gzReader.Close(); err != nil {
			pe.logger.WithError(err).Warn("Failed to close gzip reader")
		}
	}()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	var extractedFiles []string

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct file path
		filePath := filepath.Join(destDir, header.Name)

		// Security check: prevent path traversal
		if !strings.HasPrefix(filePath, destDir) {
			pe.logger.WithField("file_path", header.Name).Warn("Skipping file with suspicious path")
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(filePath, header.FileInfo().Mode()); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", filePath, err)
			}

		case tar.TypeReg:
			// Create regular file
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				return nil, fmt.Errorf("failed to create parent directory for %s: %w", filePath, err)
			}

			file, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
			if err != nil {
				return nil, fmt.Errorf("failed to create file %s: %w", filePath, err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				if err := file.Close(); err != nil {
					pe.logger.WithError(err).WithField("file_path", filePath).Warn("Failed to close file after copy error")
				}
				return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
			}
			if err := file.Close(); err != nil {
				pe.logger.WithError(err).WithField("file_path", filePath).Warn("Failed to close file after write")
			}

			extractedFiles = append(extractedFiles, header.Name)

		default:
			pe.logger.WithFields(logrus.Fields{
				"file_path": header.Name,
				"type_flag": header.Typeflag,
			}).Debug("Skipping unsupported file type")
		}
	}

	pe.logger.WithFields(logrus.Fields{
		"dest_dir":        destDir,
		"extracted_count": len(extractedFiles),
	}).Debug("Extraction completed")

	return extractedFiles, nil
}

// findAndParseManifest finds and parses the manifest.json file
func (pe *PayloadExtractor) findAndParseManifest(extractedFiles []string, extractDir string) (*Manifest, error) {
	// Find manifest.json (exact match, not substring)
	var manifestPath string
	for _, file := range extractedFiles {
		if filepath.Base(file) == "manifest.json" {
			manifestPath = filepath.Join(extractDir, file)
			break
		}
	}

	if manifestPath == "" {
		return nil, fmt.Errorf("manifest.json not found in payload")
	}

	pe.logger.WithField("manifest_path", manifestPath).Debug("Found manifest file")

	// Read and parse manifest
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Validate required fields
	if manifest.UUID == "" {
		return nil, fmt.Errorf("manifest UUID is missing")
	}
	if manifest.ClusterID == "" {
		return nil, fmt.Errorf("manifest cluster_id is missing")
	}

	pe.logger.WithFields(logrus.Fields{
		"manifest_uuid":   manifest.UUID,
		"cluster_id":      manifest.ClusterID,
		"files_count":     len(manifest.Files),
		"ros_files_count": len(manifest.ResourceOptimizationFiles),
	}).Debug("Parsed manifest successfully")

	return &manifest, nil
}

// identifyROSFiles identifies ROS CSV files from the manifest
func (pe *PayloadExtractor) identifyROSFiles(manifest *Manifest, extractedFiles []string, extractDir string) (map[string]string, error) {
	rosFiles := make(map[string]string)

	// Check if there are any ROS files specified in manifest
	if len(manifest.ResourceOptimizationFiles) == 0 {
		pe.logger.Debug("No ROS files specified in manifest")
		return nil, fmt.Errorf("no ROS files specified in manifest")
	}

	// Map extracted files for easy lookup
	extractedFileSet := make(map[string]string)
	for _, file := range extractedFiles {
		extractedFileSet[filepath.Base(file)] = file
	}

	// Find ROS files that were actually extracted
	for _, rosFileName := range manifest.ResourceOptimizationFiles {
		if extractedFile, exists := extractedFileSet[rosFileName]; exists {
			fullPath := filepath.Join(extractDir, extractedFile)
			if _, err := os.Stat(fullPath); err == nil {
				rosFiles[rosFileName] = fullPath
				pe.logger.WithFields(logrus.Fields{
					"ros_file": rosFileName,
					"path":     fullPath,
				}).Debug("Found ROS file")
			} else {
				pe.logger.WithFields(logrus.Fields{
					"ros_file": rosFileName,
					"error":    err,
				}).Warn("ROS file specified in manifest but not found")
			}
		} else {
			pe.logger.WithField("ros_file", rosFileName).Warn("ROS file specified in manifest but not extracted")
		}
	}

	if len(rosFiles) == 0 {
		return nil, fmt.Errorf("no ROS files found in payload")
	}

	pe.logger.WithField("ros_files_found", len(rosFiles)).Info("Successfully identified ROS files")
	return rosFiles, nil
}

// Cleanup removes temporary files
func (ep *ExtractedPayload) Cleanup() error {
	if ep.TempDir != "" {
		return os.RemoveAll(ep.TempDir)
	}
	return nil
}

// cleanup is a helper method to clean up on errors
func (pe *PayloadExtractor) cleanup(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		pe.logger.WithError(err).WithField("dir", dir).Error("Failed to cleanup extraction directory")
	}
}
