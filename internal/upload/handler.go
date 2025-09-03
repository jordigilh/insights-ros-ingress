package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	"github.com/RedHatInsights/insights-ros-ingress/internal/health"
	"github.com/RedHatInsights/insights-ros-ingress/internal/logger"
	"github.com/RedHatInsights/insights-ros-ingress/internal/messaging"
	"github.com/RedHatInsights/insights-ros-ingress/internal/storage"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Handler handles HCCM upload requests
type Handler struct {
	config          *config.Config
	storageClient   *storage.Client
	messagingClient *messaging.Producer
	payloadExtractor *PayloadExtractor
	logger          *logrus.Logger
}

// UploadResponse represents the response returned to clients
type UploadResponse struct {
	RequestID string     `json:"request_id"`
	Upload    UploadData `json:"upload,omitempty"`
}

// UploadData represents upload metadata in response
type UploadData struct {
	Account string `json:"account_number,omitempty"`
	OrgID   string `json:"org_id,omitempty"`
}

// Identity represents the x-rh-identity header structure
type Identity struct {
	Internal struct {
		OrgID string `json:"org_id"`
	} `json:"internal"`
	Identity struct {
		AccountNumber string `json:"account_number"`
		OrgID         string `json:"org_id"`
		Type          string `json:"type"`
	} `json:"identity"`
}

// NewHandler creates a new upload handler
func NewHandler(cfg *config.Config, storageClient *storage.Client, messagingClient *messaging.Producer, log *logrus.Logger) *Handler {
	return &Handler{
		config:           cfg,
		storageClient:    storageClient,
		messagingClient:  messagingClient,
		payloadExtractor: NewPayloadExtractor(cfg.Upload.TempDir, log),
		logger:           log,
	}
}

// HandleUpload handles the main upload endpoint
func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := h.generateRequestID()

	// Create request logger
	requestLogger := logger.WithUploadContext(h.logger, requestID, "", "")

	defer func() {
		health.HTTPRequestDuration.WithLabelValues(r.Method, "/upload").Observe(time.Since(start).Seconds())
	}()

	requestLogger.WithFields(logrus.Fields{
		"method":     r.Method,
		"user_agent": r.Header.Get("User-Agent"),
		"content_length": r.ContentLength,
	}).Info("Received upload request")

	// Validate request method
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed", requestLogger)
		return
	}

	// Handle test requests
	if h.isTestRequest(r) {
		h.handleTestRequest(w, r, requestID, requestLogger)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(h.config.Upload.MaxMemory); err != nil {
		h.respondError(w, http.StatusBadRequest, "Failed to parse multipart form", requestLogger)
		return
	}

	// Extract identity from header
	identity, err := h.extractIdentity(r)
	if err != nil && h.config.Auth.Enabled {
		h.respondError(w, http.StatusUnauthorized, "Invalid or missing identity", requestLogger)
		return
	}

	// Update logger with identity context
	if identity != nil {
		requestLogger = logger.WithUploadContext(h.logger, requestID, identity.Identity.AccountNumber, identity.Identity.OrgID)
	}

	// Get file from multipart form
	file, fileHeader, err := h.getFileFromRequest(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "File not found in request", requestLogger)
		return
	}
	defer file.Close()

	// Validate content type
	contentType := fileHeader.Header.Get("Content-Type")
	if !h.isValidContentType(contentType) {
		h.respondError(w, http.StatusUnsupportedMediaType, "Invalid content type", requestLogger)
		return
	}

	// Validate file size
	if fileHeader.Size > h.config.Upload.MaxUploadSize {
		h.respondError(w, http.StatusRequestEntityTooLarge, "File too large", requestLogger)
		return
	}

	requestLogger.WithFields(logrus.Fields{
		"content_type": contentType,
		"file_size":    fileHeader.Size,
	}).Info("Processing upload")

	// Record upload metrics
	health.UploadsTotal.WithLabelValues("received", contentType).Inc()
	health.UploadSizeBytes.WithLabelValues(contentType).Observe(float64(fileHeader.Size))

	// Process the upload
	if err := h.processUpload(r.Context(), file, requestID, identity, requestLogger); err != nil {
		health.UploadsTotal.WithLabelValues("error", contentType).Inc()
		h.respondError(w, http.StatusInternalServerError, "Failed to process upload", requestLogger)
		requestLogger.WithError(err).Error("Upload processing failed")
		return
	}

	health.UploadsTotal.WithLabelValues("success", contentType).Inc()

	// Send success response
	response := UploadResponse{
		RequestID: requestID,
	}

	if identity != nil {
		response.Upload = UploadData{
			Account: identity.Identity.AccountNumber,
			OrgID:   identity.Identity.OrgID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)

	requestLogger.Info("Upload processed successfully")
}

// processUpload handles the core upload processing logic
func (h *Handler) processUpload(ctx context.Context, file io.Reader, requestID string, identity *Identity, logger *logrus.Entry) error {
	// Extract payload
	extractedPayload, err := h.payloadExtractor.ExtractPayload(file, requestID)
	if err != nil {
		return fmt.Errorf("failed to extract payload: %w", err)
	}
	defer extractedPayload.Cleanup()

	// Validate that we have ROS files to process
	if len(extractedPayload.ROSFiles) == 0 {
		return fmt.Errorf("no ROS files found in payload")
	}

	logger.WithField("ros_files_count", len(extractedPayload.ROSFiles)).Info("Found ROS files in payload")

	// Upload ROS files to storage and collect URLs
	var uploadedFiles []string
	var objectKeys []string

	for fileName, filePath := range extractedPayload.ROSFiles {
		// Open ROS file
		rosFile, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open ROS file %s: %w", fileName, err)
		}

		// Get file info
		fileInfo, err := rosFile.Stat()
		if err != nil {
			rosFile.Close()
			return fmt.Errorf("failed to stat ROS file %s: %w", fileName, err)
		}

		// Generate storage path
		schema := h.getSchemaName(identity)
		sourceID := extractedPayload.Manifest.ClusterID
		date := extractedPayload.Manifest.Date.Format("2006-01-02")
		uploadKey := h.storageClient.GenerateUploadPath(schema, sourceID, date, fileName)

		// Prepare upload request
		uploadReq := &storage.UploadRequest{
			Key:         uploadKey,
			Data:        rosFile,
			Size:        fileInfo.Size(),
			ContentType: "text/csv",
			Metadata: map[string]string{
				"ManifestId":       extractedPayload.Manifest.UUID,
				"RequestId":        requestID,
				"ClusterUuid":      extractedPayload.Manifest.ClusterID,
				"OperatorVersion":  extractedPayload.Manifest.OperatorVersion,
			},
		}

		// Upload to storage
		uploadResult, err := h.storageClient.Upload(ctx, uploadReq)
		rosFile.Close()

		if err != nil {
			return fmt.Errorf("failed to upload ROS file %s: %w", fileName, err)
		}

		uploadedFiles = append(uploadedFiles, uploadResult.PresignedURL)
		objectKeys = append(objectKeys, uploadResult.Key)

		logger.WithFields(logrus.Fields{
			"file_name": fileName,
			"key":       uploadResult.Key,
			"size":      uploadResult.Size,
		}).Info("Successfully uploaded ROS file")
	}

	// Send ROS event message
	rosMessage := &messaging.ROSMessage{
		RequestID:   requestID,
		B64Identity: h.getB64Identity(identity),
		Metadata: messaging.ROSMetadata{
			Account:         h.getAccountID(identity),
			OrgID:           h.getOrgID(identity),
			SourceID:        extractedPayload.Manifest.ClusterID, // Using cluster ID as source ID
			ProviderUUID:    extractedPayload.Manifest.ClusterID, // Using cluster ID as provider UUID
			ClusterUUID:     extractedPayload.Manifest.ClusterID,
			ClusterAlias:    extractedPayload.Manifest.ClusterAlias,
			OperatorVersion: extractedPayload.Manifest.OperatorVersion,
		},
		Files:      uploadedFiles,
		ObjectKeys: objectKeys,
	}

	if err := h.messagingClient.SendROSEvent(ctx, rosMessage); err != nil {
		return fmt.Errorf("failed to send ROS event: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"topic":           h.config.Kafka.Topic,
		"uploaded_files":  len(uploadedFiles),
	}).Info("Successfully sent ROS event message")

	// Send validation confirmation
	if err := h.messagingClient.SendValidationMessage(ctx, requestID, "success"); err != nil {
		// Log error but don't fail the request
		logger.WithError(err).Warn("Failed to send validation message")
	}

	return nil
}

// Helper methods

func (h *Handler) generateRequestID() string {
	return uuid.New().String()
}

func (h *Handler) isTestRequest(r *http.Request) bool {
	// Check form data for test request
	if r.FormValue("test") == "test" {
		return true
	}

	// Check Content-Type for JSON test requests
	if r.Header.Get("Content-Type") == "application/json" {
		// This would need to read the body, but we'll keep it simple for now
		return false
	}

	return false
}

func (h *Handler) handleTestRequest(w http.ResponseWriter, r *http.Request, requestID string, logger *logrus.Entry) {
	logger.Info("Handling test request")

	response := UploadResponse{
		RequestID: requestID,
		Upload: UploadData{
			Account: "test-account",
			OrgID:   "test-org",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) extractIdentity(r *http.Request) (*Identity, error) {
	if !h.config.Auth.Enabled {
		return nil, nil
	}

	// In a real implementation, this would decode the x-rh-identity header
	// For now, we'll create a mock identity
	return &Identity{
		Identity: struct {
			AccountNumber string `json:"account_number"`
			OrgID         string `json:"org_id"`
			Type          string `json:"type"`
		}{
			AccountNumber: "mock-account",
			OrgID:         "mock-org",
			Type:          "User",
		},
	}, nil
}

func (h *Handler) getFileFromRequest(r *http.Request) (io.ReadCloser, *multipart.FileHeader, error) {
	// Try "file" field first, then "upload" field
	file, fileHeader, err := r.FormFile("file")
	if err == nil {
		return file, fileHeader, nil
	}

	file, fileHeader, err = r.FormFile("upload")
	if err == nil {
		return file, fileHeader, nil
	}

	return nil, nil, fmt.Errorf("no file found in request")
}

func (h *Handler) isValidContentType(contentType string) bool {
	for _, allowedType := range h.config.Upload.AllowedTypes {
		if contentType == allowedType {
			return true
		}
	}

	// Also check for gzip patterns
	gzipPattern := regexp.MustCompile(`application/(x-gzip|gzip)(; charset=binary)?`)
	if gzipPattern.MatchString(contentType) {
		return true
	}

	// Check for vnd.redhat patterns
	vndPattern := regexp.MustCompile(`application/vnd\.redhat\.([a-z0-9-]+)\.([a-z0-9-]+).*`)
	return vndPattern.MatchString(contentType)
}

func (h *Handler) getSchemaName(identity *Identity) string {
	if identity != nil && identity.Identity.OrgID != "" {
		return fmt.Sprintf("org_%s", identity.Identity.OrgID)
	}
	return "default"
}

func (h *Handler) getAccountID(identity *Identity) string {
	if identity != nil {
		return identity.Identity.AccountNumber
	}
	return "unknown"
}

func (h *Handler) getOrgID(identity *Identity) string {
	if identity != nil {
		return identity.Identity.OrgID
	}
	return "unknown"
}

func (h *Handler) getB64Identity(identity *Identity) string {
	// In a real implementation, this would return the original base64 identity
	// For now, return empty string
	return ""
}

func (h *Handler) respondError(w http.ResponseWriter, statusCode int, message string, logger *logrus.Entry) {
	health.HTTPRequestsTotal.WithLabelValues("POST", "/upload", strconv.Itoa(statusCode)).Inc()

	logger.WithFields(logrus.Fields{
		"status_code": statusCode,
		"error":       message,
	}).Warn("Request failed")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := map[string]string{
		"error": message,
	}
	json.NewEncoder(w).Encode(errorResponse)
}