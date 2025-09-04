package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gmeasure"

	"github.com/RedHatInsights/insights-ros-ingress/internal/auth"
	"github.com/RedHatInsights/insights-ros-ingress/internal/auth/mocks"
)

var _ = Describe("Kubernetes Auth Middleware", func() {
	var (
		ctrl              *gomock.Controller
		mockAuthClient    *mocks.MockAuthenticationV1Interface
		mockTokenReviewer *mocks.MockTokenReviewInterface
		log               *logrus.Logger
		middleware        func(http.Handler) http.Handler
		handler           http.Handler
		req               *http.Request
		rr                *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAuthClient = mocks.NewMockAuthenticationV1Interface(ctrl)
		mockTokenReviewer = mocks.NewMockTokenReviewInterface(ctrl)
		log = logrus.New()
		log.SetLevel(logrus.ErrorLevel) // Reduce noise in tests
		rr = httptest.NewRecorder()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("Authentication Header Validation", func() {
		Context("When Authorization header is missing", func() {
			BeforeEach(func() {
				middleware = auth.AuthMiddleware(mockAuthClient, log)
				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				req = httptest.NewRequest("GET", "/test", nil)
			})

			It("should return 401 Unauthorized", func() {
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusUnauthorized))
				Expect(rr.Body.String()).To(Equal("Unauthorized: Missing Authorization header\n"))
			})
		})

		Context("When Authorization header has invalid format", func() {
			BeforeEach(func() {
				middleware = auth.AuthMiddleware(mockAuthClient, log)
				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
			})

			It("should return 401 Unauthorized", func() {
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusUnauthorized))
				Expect(rr.Body.String()).To(Equal("Unauthorized: Invalid Authorization header format\n"))
			})
		})

		Context("When Bearer token is empty", func() {
			BeforeEach(func() {
				middleware = auth.AuthMiddleware(mockAuthClient, log)
				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer ")
			})

			It("should return 401 Unauthorized", func() {
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusUnauthorized))
				Expect(rr.Body.String()).To(Equal("Unauthorized: Empty token\n"))
			})
		})
	})

	Describe("Token Validation", func() {
		Context("When token is valid and user is authenticated", func() {
			var capturedUser *authenticationv1.UserInfo
			var capturedToken string

			BeforeEach(func() {
				// Setup mock expectations
				mockAuthClient.EXPECT().TokenReviews().Return(mockTokenReviewer)

				expectedResponse := &authenticationv1.TokenReview{
					Status: authenticationv1.TokenReviewStatus{
						Authenticated: true,
						User: authenticationv1.UserInfo{
							Username: "test-user",
							UID:      "test-uid",
							Groups:   []string{"system:authenticated"},
						},
					},
				}

				mockTokenReviewer.EXPECT().Create(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, tokenReview *authenticationv1.TokenReview, opts metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
					result := expectedResponse.DeepCopy()
					result.Spec = tokenReview.Spec
					return result, nil
				})

				middleware = auth.AuthMiddleware(mockAuthClient, log)

				capturedUser = nil
				capturedToken = ""

				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if user := r.Context().Value(auth.AuthenticatedUserKey); user != nil {
						if userInfo, ok := user.(authenticationv1.UserInfo); ok {
							capturedUser = &userInfo
						}
					}
					if token := r.Context().Value(auth.OauthTokenKey); token != nil {
						if tokenStr, ok := token.(string); ok {
							capturedToken = tokenStr
						}
					}
					w.WriteHeader(http.StatusOK)
				}))

				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer valid-token")
			})

			It("should allow the request and add user info to context", func() {
				handler.ServeHTTP(rr, req)

				Expect(rr.Code).To(Equal(http.StatusOK))
				Expect(capturedUser).ToNot(BeNil())
				Expect(capturedUser.Username).To(Equal("test-user"))
				Expect(capturedUser.UID).To(Equal("test-uid"))
				Expect(capturedToken).To(Equal("valid-token"))
			})
		})

		Context("When token is invalid", func() {
			BeforeEach(func() {
				// Setup mock expectations
				mockAuthClient.EXPECT().TokenReviews().Return(mockTokenReviewer)

				expectedResponse := &authenticationv1.TokenReview{
					Status: authenticationv1.TokenReviewStatus{
						Authenticated: false,
						Error:         "token not found",
					},
				}

				mockTokenReviewer.EXPECT().Create(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).DoAndReturn(func(ctx context.Context, tokenReview *authenticationv1.TokenReview, opts metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
					result := expectedResponse.DeepCopy()
					result.Spec = tokenReview.Spec
					return result, nil
				})

				middleware = auth.AuthMiddleware(mockAuthClient, log)
				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer invalid-token")
			})

			It("should return 401 Unauthorized", func() {
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusUnauthorized))
				Expect(rr.Body.String()).To(Equal("Unauthorized: Invalid token\n"))
			})
		})

		Context("When TokenReview API returns an error", func() {
			BeforeEach(func() {
				// Setup mock expectations for error case
				mockAuthClient.EXPECT().TokenReviews().Return(mockTokenReviewer)
				mockTokenReviewer.EXPECT().Create(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil, &mockError{message: "TokenReview API error"})

				middleware = auth.AuthMiddleware(mockAuthClient, log)
				handler = middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				req = httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", "Bearer error-token")
			})

			It("should return 500 Internal Server Error", func() {
				handler.ServeHTTP(rr, req)
				Expect(rr.Code).To(Equal(http.StatusInternalServerError))
				Expect(rr.Body.String()).To(Equal("Internal Server Error: Authentication failed\n"))
			})
		})
	})
})

var _ = Describe("Context Keys", func() {
	It("should have properly typed context keys", func() {
		Expect(auth.AuthenticatedUserKey).To(Equal(auth.ContextKey("authenticated_user")))
		Expect(auth.OauthTokenKey).To(Equal(auth.ContextKey("oauth_token")))
	})

	It("should properly store and retrieve values from context", func() {
		ctx := context.Background()

		userInfo := authenticationv1.UserInfo{Username: "test"}
		token := "test-token"

		ctx = context.WithValue(ctx, auth.AuthenticatedUserKey, userInfo)
		ctx = context.WithValue(ctx, auth.OauthTokenKey, token)

		retrievedUser := ctx.Value(auth.AuthenticatedUserKey)
		retrievedToken := ctx.Value(auth.OauthTokenKey)

		if retrievedUserInfo, ok := retrievedUser.(authenticationv1.UserInfo); !ok {
			Fail("Expected user info in context")
		} else {
			Expect(retrievedUserInfo.Username).To(Equal(userInfo.Username))
		}

		Expect(retrievedToken).To(Equal(token))
	})
})

var _ = Describe("Kubernetes Config Loading", func() {
	var log *logrus.Logger

	BeforeEach(func() {
		log = logrus.New()
		log.SetLevel(logrus.ErrorLevel)
	})

	Context("When no KUBECONFIG is set and not in cluster", func() {
		It("should return an error", func() {
			config, err := auth.GetKubernetesConfig(log)

			// In test environment, this should fail because we're not in cluster
			Expect(err).To(HaveOccurred())
			Expect(config).To(BeNil())
		})
	})
})

var _ = Describe("Middleware Performance", func() {
	It("should handle authentication requests efficiently", func() {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()

		mockAuthClient := mocks.NewMockAuthenticationV1Interface(ctrl)
		mockTokenReviewer := mocks.NewMockTokenReviewInterface(ctrl)
		log := logrus.New()
		log.SetLevel(logrus.ErrorLevel)

		// Setup mock expectations for performance test
		mockAuthClient.EXPECT().TokenReviews().Return(mockTokenReviewer).AnyTimes()

		expectedResponse := &authenticationv1.TokenReview{
			Status: authenticationv1.TokenReviewStatus{
				Authenticated: true,
				User: authenticationv1.UserInfo{
					Username: "bench-user",
					UID:      "bench-uid",
				},
			},
		}

		mockTokenReviewer.EXPECT().Create(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).DoAndReturn(func(ctx context.Context, tokenReview *authenticationv1.TokenReview, opts metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
			result := expectedResponse.DeepCopy()
			result.Spec = tokenReview.Spec
			return result, nil
		}).AnyTimes()

		middleware := auth.AuthMiddleware(mockAuthClient, log)
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		// Use gmeasure for performance testing
		experiment := gmeasure.NewExperiment("middleware performance")
		AddReportEntry(experiment.Name, experiment)

		experiment.MeasureDuration("request_duration", func() {
			for i := 0; i < 1000; i++ {
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
			}
		})

		// Verify performance meets expectations
		medianDuration := experiment.GetStats("request_duration").DurationFor(gmeasure.StatMedian)
		Expect(medianDuration).To(BeNumerically("<", time.Second), "Should complete 1000 requests in under 1 second")
	})
})

// mockError implements error interface for testing error scenarios
type mockError struct {
	message string
}

func (e *mockError) Error() string {
	return "TokenReview API error"
}
