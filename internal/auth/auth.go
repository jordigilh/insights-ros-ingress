package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authenticationv1client "k8s.io/client-go/kubernetes/typed/authentication/v1"
)

type ContextKey string

const (
	AuthenticatedUserKey ContextKey = "authenticated_user"
	OauthTokenKey        ContextKey = "oauth_token"
)

// KubernetesAuthMiddleware creates middleware that validates tokens using Kubernetes TokenReviewer API
func KubernetesAuthMiddleware(log *logrus.Logger) func(http.Handler) http.Handler {
	// Initialize Kubernetes client once - try KUBECONFIG first, then in-cluster
	config, err := GetKubernetesConfig(log)
	if err != nil {
		log.WithError(err).Fatal("Failed to get Kubernetes configuration")
	}

	authClient, err := authenticationv1client.NewForConfig(config)
	if err != nil {
		log.WithError(err).Fatal("Failed to create Kubernetes authentication client")
	}
	return AuthMiddleware(authClient, log)
}

// getKubernetesConfig attempts to load Kubernetes config from KUBECONFIG env var first,
// then falls back to in-cluster config
func GetKubernetesConfig(log *logrus.Logger) (*rest.Config, error) {
	// First, try to get config from KUBECONFIG environment variable
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath != "" {
		log.WithField("kubeconfig", kubeconfigPath).Info("Using KUBECONFIG from environment variable")
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err == nil {
			return config, nil
		}
		log.WithError(err).Warn("Failed to load config from KUBECONFIG, trying in-cluster config")
	}

	// Fall back to in-cluster config
	log.Info("Using in-cluster Kubernetes configuration")
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get both KUBECONFIG and in-cluster config: %w", err)
	}

	return config, nil
}

var AuthMiddleware = func(authClient authenticationv1client.AuthenticationV1Interface, log *logrus.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Debug("Missing Authorization header")
				http.Error(w, "Unauthorized: Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Check Bearer token format
			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				log.Debug("Invalid Authorization header format - must be 'Bearer <token>'")
				http.Error(w, "Unauthorized: Invalid Authorization header format", http.StatusUnauthorized)
				return
			}

			// Extract token
			token := strings.TrimPrefix(authHeader, bearerPrefix)
			if token == "" {
				log.Debug("Empty token in Authorization header")
				http.Error(w, "Unauthorized: Empty token", http.StatusUnauthorized)
				return
			}

			// Create TokenReview request
			tokenReview := &authenticationv1.TokenReview{
				Spec: authenticationv1.TokenReviewSpec{
					Token: token,
				},
			}

			// Perform TokenReview with timeout
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			result, err := authClient.TokenReviews().Create(ctx, tokenReview, metav1.CreateOptions{})
			if err != nil {
				log.WithError(err).Error("TokenReview API call failed")
				http.Error(w, "Internal Server Error: Authentication failed", http.StatusInternalServerError)
				return
			}

			// Validate authentication result
			if !result.Status.Authenticated {
				log.WithFields(logrus.Fields{
					"error": result.Status.Error,
				}).Info("Token authentication failed")
				http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
				return
			}

			// Log successful authentication
			log.WithFields(logrus.Fields{
				"user": result.Status.User.Username,
				"uid":  result.Status.User.UID,
			}).Debug("Token authentication successful")

			// Add user info to request context for downstream handlers
			userCtx := context.WithValue(r.Context(), AuthenticatedUserKey, result.Status.User)
			// Add oauth token to request context for downstream handlers (used in kafka messages to ROS to authenticate the request)
			oauthTokenCtx := context.WithValue(userCtx, OauthTokenKey, token)
			r = r.WithContext(oauthTokenCtx)

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}

}
