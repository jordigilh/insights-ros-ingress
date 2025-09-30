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
	"strings"
	"time"

	"github.com/RedHatInsights/insights-ros-ingress/internal/auth"
	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	"github.com/RedHatInsights/insights-ros-ingress/internal/health"
	"github.com/RedHatInsights/insights-ros-ingress/internal/logger"
	"github.com/RedHatInsights/insights-ros-ingress/internal/messaging"
	"github.com/RedHatInsights/insights-ros-ingress/internal/storage"
	"github.com/google/uuid"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"github.com/sirupsen/logrus"
	authenticationv1 "k8s.io/api/authentication/v1"
)

// Handler handles HCCM upload requests
type Handler struct {
	config           *config.Config
	storageClient    *storage.Client
	messagingClient  *messaging.Producer
	payloadExtractor *PayloadExtractor
	logger           *logrus.Logger
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

// NewHandler creates a new upload handler
// Authentication is expected to be handled by middleware that stores user info in request context
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
		"method":         r.Method,
		"user_agent":     r.Header.Get("User-Agent"),
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
		requestLogger = logger.WithUploadContext(h.logger, requestID, identity.AccountNumber, identity.OrgID)
	}

	// Get file from multipart form
	file, fileHeader, err := h.getFileFromRequest(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "File not found in request", requestLogger)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			requestLogger.WithError(err).Warn("Failed to close uploaded file")
		}
	}()

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
			Account: identity.AccountNumber,
			OrgID:   identity.OrgID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		requestLogger.WithError(err).Error("Failed to encode response")
	}

	requestLogger.Info("Upload processed successfully")
}

// processUpload handles the core upload processing logic
func (h *Handler) processUpload(ctx context.Context, file io.Reader, requestID string, identity *identity.Identity, logger *logrus.Entry) error {
	// Extract payload
	extractedPayload, err := h.payloadExtractor.ExtractPayload(file, requestID)
	if err != nil {
		return fmt.Errorf("failed to extract payload: %w", err)
	}
	defer func() {
		if err := extractedPayload.Cleanup(); err != nil {
			logger.WithError(err).Warn("Failed to cleanup extracted payload")
		}
	}()

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
			if closeErr := rosFile.Close(); closeErr != nil {
				logger.WithError(closeErr).Warn("Failed to close ROS file after stat error")
			}
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
				"ManifestId":      extractedPayload.Manifest.UUID,
				"RequestId":       requestID,
				"ClusterUuid":     extractedPayload.Manifest.ClusterID,
				"OperatorVersion": extractedPayload.Manifest.OperatorVersion,
			},
		}

		// Upload to storage
		uploadResult, err := h.storageClient.Upload(ctx, uploadReq)
		if closeErr := rosFile.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close ROS file after upload")
		}

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

	token, err := h.getOAuthTokenFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get OAuth token from context: %w", err)
	}
	// Send ROS event message
	rosMessage := &messaging.ROSMessage{
		RequestID:   requestID,
		B64Identity: token,
		Metadata: messaging.ROSMetadata{
			Account:         h.getAccountID(identity),
			OrgID:           h.getOrgID(identity),
			SourceID:        extractedPayload.Manifest.ClusterID, // Using cluster ID as source ID
			ProviderUUID:    extractedPayload.Manifest.ClusterID, // Using cluster ID as provider UUID
			ClusterUUID:     extractedPayload.Manifest.ClusterID,
			ClusterAlias:    h.getClusterAlias(extractedPayload.Manifest),
			OperatorVersion: extractedPayload.Manifest.OperatorVersion,
		},
		Files:      uploadedFiles,
		ObjectKeys: objectKeys,
	}

	if err := h.messagingClient.SendROSEvent(ctx, rosMessage); err != nil {
		return fmt.Errorf("failed to send ROS event: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"topic":          h.config.Kafka.Topic,
		"uploaded_files": len(uploadedFiles),
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

// getClusterAlias returns the cluster alias from manifest, falling back to cluster ID
// This matches koku's behavior: prefer explicit alias, fallback to cluster ID
func (h *Handler) getClusterAlias(manifest *Manifest) string {
	if manifest.ClusterAlias != "" {
		return manifest.ClusterAlias
	}
	// Fallback to cluster ID if no explicit alias is provided
	// This matches koku's get_cluster_alias() behavior
	return manifest.ClusterID
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

func (h *Handler) handleTestRequest(w http.ResponseWriter, _ *http.Request, requestID string, logger *logrus.Entry) {
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.WithError(err).Error("Failed to encode test response")
	}
}

func (h *Handler) extractIdentity(r *http.Request) (*identity.Identity, error) {
	if !h.config.Auth.Enabled {
		return nil, nil
	}

	// Get authenticated user from request context (set by auth middleware)
	user, err := h.getAuthenticatedUserFromContext(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticated user from context: %w", err)
	}

	h.logger.WithFields(logrus.Fields{
		"user": user.Username,
		"uid":  user.UID,
	}).Debug("Retrieved authenticated user from context")

	// Create identity from OAuth2 user information
	return h.createIdentityFromOAuth2User(user), nil
}

// getAuthenticatedUserFromContext retrieves the authenticated user from request context
func (h *Handler) getAuthenticatedUserFromContext(ctx context.Context) (*authenticationv1.UserInfo, error) {
	userValue := ctx.Value(auth.AuthenticatedUserKey)
	if userValue == nil {
		return nil, fmt.Errorf("no authenticated user found in context - ensure auth middleware is properly configured")
	}

	user, ok := userValue.(authenticationv1.UserInfo)
	if !ok {
		return nil, fmt.Errorf("invalid user type in context")
	}

	return &user, nil
}

// getOAuthTokenFromContext retrieves the OAuth token from request context (if needed for downstream services)
func (h *Handler) getOAuthTokenFromContext(ctx context.Context) (string, error) {
	tokenValue := ctx.Value(auth.OauthTokenKey)
	if tokenValue == nil {
		return "", fmt.Errorf("no OAuth token found in context")
	}

	token, ok := tokenValue.(string)
	if !ok {
		return "", fmt.Errorf("invalid token type in context")
	}

	return token, nil
}

// createIdentityFromOAuth2User creates an identity from OAuth2/Kubernetes user information
// This supports tokens issued by Keycloak or Kubernetes API server
func (h *Handler) createIdentityFromOAuth2User(user *authenticationv1.UserInfo) *identity.Identity {
	// Extract organization ID and account number from user information
	// Adjust these extraction methods based on your OAuth2 provider (Keycloak/K8s API)

	orgID := h.extractOrgIDFromUser(user)
	accountNumber := h.extractAccountNumberFromUser(user)

	// Determine token type based on username pattern
	tokenType := "User"
	if strings.HasPrefix(user.Username, "system:serviceaccount:") {
		tokenType = "ServiceAccount"
	}

	return &identity.Identity{
		AccountNumber: accountNumber,
		OrgID:         orgID,
		Type:          tokenType,
		AuthType:      "oauth2",
		User: &identity.User{
			Username:  user.Username,
			Email:     h.extractEmailFromUser(user),
			FirstName: h.extractFirstNameFromUser(user),
			LastName:  h.extractLastNameFromUser(user),
			Active:    true,
			OrgAdmin:  h.isOrgAdminUser(user),
			Internal:  h.isInternalUser(user),
			Locale:    "en_US",
		},
		Internal: identity.Internal{
			OrgID: orgID,
		},
	}
}

// Helper methods to extract information from OAuth2 user
// Customize these based on your OAuth2 provider (Keycloak, Kubernetes API, etc.)

func (h *Handler) extractOrgIDFromUser(user *authenticationv1.UserInfo) string {
	// Look for org ID in user groups (common in Keycloak/K8s RBAC)
	for _, group := range user.Groups {
		if strings.HasPrefix(group, "org:") {
			orgID := strings.TrimPrefix(group, "org:")
			if orgID != "" { // Skip empty org IDs
				return orgID
			}
		}
	}

	// Check extra fields (Keycloak custom claims, K8s annotations)
	if orgIDExtra, exists := user.Extra["org_id"]; exists && len(orgIDExtra) > 0 {
		return orgIDExtra[0]
	}

	// For Keycloak, you might also check:
	// - user.Extra["organization"]
	// - user.Extra["tenant_id"]

	// Default fallback - consider making this configurable
	return "1"
}

func (h *Handler) extractAccountNumberFromUser(user *authenticationv1.UserInfo) string {
	// Check extra fields (Keycloak custom claims, K8s annotations)
	if accountExtra, exists := user.Extra["account_number"]; exists && len(accountExtra) > 0 {
		return accountExtra[0]
	}

	// Check for Keycloak alternative fields
	if customerIDExtra, exists := user.Extra["customer_id"]; exists && len(customerIDExtra) > 0 {
		return customerIDExtra[0]
	}

	if clientIDExtra, exists := user.Extra["client_id"]; exists && len(clientIDExtra) > 0 {
		return clientIDExtra[0]
	}

	// Look for account in user groups (RBAC mapping)
	for _, group := range user.Groups {
		if strings.HasPrefix(group, "account:") {
			return strings.TrimPrefix(group, "account:")
		}
	}

	// Could also parse from username (e.g., "user@account123") if needed

	// Default fallback - consider making this configurable
	return "1"
}

func (h *Handler) extractEmailFromUser(user *authenticationv1.UserInfo) string {
	if emailExtra, exists := user.Extra["email"]; exists && len(emailExtra) > 0 {
		return emailExtra[0]
	}
	return ""
}

func (h *Handler) extractFirstNameFromUser(user *authenticationv1.UserInfo) string {
	if firstNameExtra, exists := user.Extra["first_name"]; exists && len(firstNameExtra) > 0 {
		return firstNameExtra[0]
	}
	return ""
}

func (h *Handler) extractLastNameFromUser(user *authenticationv1.UserInfo) string {
	if lastNameExtra, exists := user.Extra["last_name"]; exists && len(lastNameExtra) > 0 {
		return lastNameExtra[0]
	}
	return ""
}

func (h *Handler) isOrgAdminUser(user *authenticationv1.UserInfo) bool {
	for _, group := range user.Groups {
		if group == "org-admin" || strings.Contains(group, "admin") {
			return true
		}
	}
	return false
}

func (h *Handler) isInternalUser(user *authenticationv1.UserInfo) bool {
	for _, group := range user.Groups {
		if group == "internal" || strings.Contains(group, "redhat") {
			return true
		}
	}
	return false
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

func (h *Handler) getSchemaName(identity *identity.Identity) string {
	if identity != nil && identity.OrgID != "" {
		return fmt.Sprintf("org_%s", identity.OrgID)
	}
	return "default"
}

func (h *Handler) getAccountID(identity *identity.Identity) string {
	if identity != nil {
		return identity.AccountNumber
	}
	return "unknown"
}

func (h *Handler) getOrgID(identity *identity.Identity) string {
	if identity != nil {
		if identity.OrgID == "" {
			return identity.Internal.OrgID
		}
		return identity.OrgID
	}
	return "unknown"
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
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		logger.WithError(err).Error("Failed to encode error response")
	}
}
