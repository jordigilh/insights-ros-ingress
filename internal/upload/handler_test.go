package upload

import (
	"context"
	"net/http"

	"github.com/RedHatInsights/insights-ros-ingress/internal/auth"
	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	authenticationv1 "k8s.io/api/authentication/v1"
)

var _ = Describe("Handler OAuth2 Authentication", func() {
	var (
		handler *Handler
		logger  *logrus.Logger
	)

	BeforeEach(func() {
		logger = logrus.New()
		logger.SetLevel(logrus.ErrorLevel) // Suppress logs during tests
	})

	Describe("extractIdentity", func() {
		Context("when auth is disabled", func() {
			BeforeEach(func() {
				cfg := &config.Config{
					Auth: config.AuthConfig{
						Enabled: false,
					},
				}
				handler = NewHandler(cfg, nil, nil, logger)
			})

			It("should return nil", func() {
				ctx := context.WithValue(context.Background(), auth.AuthenticatedUserKey, authenticationv1.UserInfo{
					Username: "test-user",
					UID:      "test-uid",
				})
				req := &http.Request{}
				req = req.WithContext(ctx)

				result, err := handler.extractIdentity(req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(BeNil())
			})
		})

		Context("when auth is enabled", func() {
			BeforeEach(func() {
				cfg := &config.Config{
					Auth: config.AuthConfig{
						Enabled: true,
					},
				}
				handler = NewHandler(cfg, nil, nil, logger)
			})

			Context("with valid user in context", func() {
				It("should create identity successfully", func() {
					user := authenticationv1.UserInfo{
						Username: "test-user",
						UID:      "test-uid",
						Groups:   []string{"org:123", "account:456"},
						Extra: map[string]authenticationv1.ExtraValue{
							"email": {"test@example.com"},
						},
					}
					ctx := context.WithValue(context.Background(), auth.AuthenticatedUserKey, user)
					req := &http.Request{}
					req = req.WithContext(ctx)

					result, err := handler.extractIdentity(req)

					Expect(err).ToNot(HaveOccurred())
					Expect(result).ToNot(BeNil())
					Expect(result.AccountNumber).To(Equal("456"))
					Expect(result.OrgID).To(Equal("123"))
					Expect(result.Type).To(Equal("User"))
					Expect(result.AuthType).To(Equal("oauth2"))
					Expect(result.User.Username).To(Equal("test-user"))
					Expect(result.User.Email).To(Equal("test@example.com"))
					Expect(result.Internal.OrgID).To(Equal("123"))
				})
			})

			Context("with service account user", func() {
				It("should set correct type", func() {
					user := authenticationv1.UserInfo{
						Username: "system:serviceaccount:default:my-service",
						UID:      "service-uid",
						Groups:   []string{"org:789"},
					}
					ctx := context.WithValue(context.Background(), auth.AuthenticatedUserKey, user)
					req := &http.Request{}
					req = req.WithContext(ctx)

					result, err := handler.extractIdentity(req)

					Expect(err).ToNot(HaveOccurred())
					Expect(result).ToNot(BeNil())
					Expect(result.Type).To(Equal("ServiceAccount"))
					Expect(result.OrgID).To(Equal("789"))
					Expect(result.AccountNumber).To(Equal("1")) // default fallback
					Expect(result.User.Username).To(Equal("system:serviceaccount:default:my-service"))
				})
			})

			Context("with missing user in context", func() {
				It("should return error", func() {
					req := &http.Request{}
					req = req.WithContext(context.Background())

					result, err := handler.extractIdentity(req)

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("no authenticated user found in context"))
					Expect(result).To(BeNil())
				})
			})
		})
	})

	Describe("getAuthenticatedUserFromContext", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("with valid user info", func() {
			It("should return successfully", func() {
				expectedUser := authenticationv1.UserInfo{
					Username: "test-user",
					UID:      "test-uid",
					Groups:   []string{"group1", "group2"},
				}
				ctx := context.WithValue(context.Background(), auth.AuthenticatedUserKey, expectedUser)

				result, err := handler.getAuthenticatedUserFromContext(ctx)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result.Username).To(Equal(expectedUser.Username))
				Expect(result.UID).To(Equal(expectedUser.UID))
				Expect(result.Groups).To(Equal(expectedUser.Groups))
			})
		})

		Context("with missing context value", func() {
			It("should return error", func() {
				ctx := context.Background()

				result, err := handler.getAuthenticatedUserFromContext(ctx)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no authenticated user found in context"))
				Expect(result).To(BeNil())
			})
		})

		// Type validation tests removed - covered in auth_test.go middleware tests
	})

	Describe("getOAuthTokenFromContext", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("with valid token string", func() {
			It("should return successfully", func() {
				expectedToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."
				ctx := context.WithValue(context.Background(), auth.OauthTokenKey, expectedToken)

				result, err := handler.getOAuthTokenFromContext(ctx)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(expectedToken))
			})
		})

		Context("with missing token in context", func() {
			It("should return error", func() {
				ctx := context.Background()

				result, err := handler.getOAuthTokenFromContext(ctx)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no OAuth token found in context"))
				Expect(result).To(BeEmpty())
			})
		})

		// OAuth token type validation tests removed - covered in auth_test.go middleware tests

		Context("with empty token", func() {
			It("should still return successfully", func() {
				ctx := context.WithValue(context.Background(), auth.OauthTokenKey, "")

				result, err := handler.getOAuthTokenFromContext(ctx)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(BeEmpty())
			})
		})
	})

	Describe("createIdentityFromOAuth2User", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("with regular user with complete info", func() {
			It("should create proper identity", func() {
				user := &authenticationv1.UserInfo{
					Username: "john.doe",
					UID:      "user-123",
					Groups:   []string{"org:456", "account:789", "team-lead"},
					Extra: map[string]authenticationv1.ExtraValue{
						"email":      {"john.doe@example.com"},
						"first_name": {"John"},
						"last_name":  {"Doe"},
					},
				}

				result := handler.createIdentityFromOAuth2User(user)

				Expect(result).ToNot(BeNil())
				Expect(result.AccountNumber).To(Equal("789"))
				Expect(result.OrgID).To(Equal("456"))
				Expect(result.Type).To(Equal("User"))
				Expect(result.AuthType).To(Equal("oauth2"))
				Expect(result.User.Username).To(Equal("john.doe"))
				Expect(result.User.Email).To(Equal("john.doe@example.com"))
				Expect(result.User.FirstName).To(Equal("John"))
				Expect(result.User.LastName).To(Equal("Doe"))
				Expect(result.User.Active).To(BeTrue())
				Expect(result.User.OrgAdmin).To(BeFalse())
				Expect(result.User.Internal).To(BeFalse())
				Expect(result.User.Locale).To(Equal("en_US"))
				Expect(result.Internal.OrgID).To(Equal("456"))
			})
		})

		Context("with service account user", func() {
			It("should set correct type", func() {
				user := &authenticationv1.UserInfo{
					Username: "system:serviceaccount:kube-system:my-service",
					UID:      "service-456",
					Groups:   []string{"org:999"},
				}

				result := handler.createIdentityFromOAuth2User(user)

				Expect(result).ToNot(BeNil())
				Expect(result.Type).To(Equal("ServiceAccount"))
				Expect(result.AccountNumber).To(Equal("1")) // default fallback
				Expect(result.OrgID).To(Equal("999"))
				Expect(result.User.Username).To(Equal("system:serviceaccount:kube-system:my-service"))
			})
		})

		Context("with admin user with internal flag", func() {
			It("should set admin and internal flags", func() {
				user := &authenticationv1.UserInfo{
					Username: "admin.user",
					UID:      "admin-789",
					Groups:   []string{"org:111", "org-admin", "internal", "account:222"},
				}

				result := handler.createIdentityFromOAuth2User(user)

				Expect(result).ToNot(BeNil())
				Expect(result.AccountNumber).To(Equal("222"))
				Expect(result.OrgID).To(Equal("111"))
				Expect(result.User.OrgAdmin).To(BeTrue())
				Expect(result.User.Internal).To(BeTrue())
			})
		})

		Context("with minimal user with defaults", func() {
			It("should use default values", func() {
				user := &authenticationv1.UserInfo{
					Username: "minimal.user",
					UID:      "min-123",
				}

				result := handler.createIdentityFromOAuth2User(user)

				Expect(result).ToNot(BeNil())
				Expect(result.AccountNumber).To(Equal("1")) // default fallback
				Expect(result.OrgID).To(Equal("1"))         // default fallback
				Expect(result.Type).To(Equal("User"))
				Expect(result.User.Username).To(Equal("minimal.user"))
				Expect(result.User.Active).To(BeTrue())
			})
		})
	})

	Describe("extractOrgIDFromUser", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("when org is in groups", func() {
			It("should extract from groups", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "org:123", "other-group"},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("123"))
			})
		})

		Context("when org is in extra fields", func() {
			It("should extract from extra fields", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"org_id": {"456"},
					},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("456"))
			})
		})

		Context("when both groups and extra fields have org", func() {
			It("should prioritize groups over extra fields", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"org:789"},
					Extra: map[string]authenticationv1.ExtraValue{
						"org_id": {"456"},
					},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("789"))
			})
		})

		Context("when multiple org groups exist", func() {
			It("should use first org group", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"org:111", "org:222", "org:333"},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("111"))
			})
		})

		Context("when no org is found", func() {
			It("should return default fallback", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "admin"},
					Extra: map[string]authenticationv1.ExtraValue{
						"email": {"test@example.com"},
					},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("1"))
			})
		})

		Context("with empty user", func() {
			It("should return default", func() {
				user := &authenticationv1.UserInfo{}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("1"))
			})
		})

		Context("with malformed groups", func() {
			It("should ignore malformed groups and find valid one", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"org:", "org:valid-123", "not-org-group"},
				}

				result := handler.extractOrgIDFromUser(user)

				Expect(result).To(Equal("valid-123"))
			})
		})
	})

	Describe("extractAccountNumberFromUser", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("when account is in extra fields", func() {
			It("should extract from extra fields", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"account_number": {"123456"},
					},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("123456"))
			})
		})

		Context("when account is in groups", func() {
			It("should extract from groups", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "account:789", "other-group"},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("789"))
			})
		})

		Context("when both extra fields and groups have account", func() {
			It("should prioritize extra fields over groups", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"account:999"},
					Extra: map[string]authenticationv1.ExtraValue{
						"account_number": {"111"},
					},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("111"))
			})
		})

		Context("when customer_id is available", func() {
			It("should use customer_id as alternative", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"customer_id": {"555"},
					},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("555"))
			})
		})

		Context("when client_id is available", func() {
			It("should use client_id as alternative", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"client_id": {"777"},
					},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("777"))
			})
		})

		Context("when no account is found", func() {
			It("should return default fallback", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "org:123"},
					Extra: map[string]authenticationv1.ExtraValue{
						"email": {"test@example.com"},
					},
				}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("1"))
			})
		})

		Context("with empty user", func() {
			It("should return default", func() {
				user := &authenticationv1.UserInfo{}

				result := handler.extractAccountNumberFromUser(user)

				Expect(result).To(Equal("1"))
			})
		})
	})

	Describe("extractEmailFromUser", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("when email exists in extra fields", func() {
			It("should extract email", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"email": {"test@example.com"},
					},
				}

				result := handler.extractEmailFromUser(user)

				Expect(result).To(Equal("test@example.com"))
			})
		})

		Context("when email is missing", func() {
			It("should return empty string", func() {
				user := &authenticationv1.UserInfo{
					Extra: map[string]authenticationv1.ExtraValue{
						"first_name": {"John"},
					},
				}

				result := handler.extractEmailFromUser(user)

				Expect(result).To(BeEmpty())
			})
		})

		Context("with empty user", func() {
			It("should return empty string", func() {
				user := &authenticationv1.UserInfo{}

				result := handler.extractEmailFromUser(user)

				Expect(result).To(BeEmpty())
			})
		})
	})

	Describe("isOrgAdminUser", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("with org-admin group", func() {
			It("should return true", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "org-admin", "users"},
				}

				result := handler.isOrgAdminUser(user)

				Expect(result).To(BeTrue())
			})
		})

		Context("with group containing admin", func() {
			It("should return true", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"cluster-admin", "users"},
				}

				result := handler.isOrgAdminUser(user)

				Expect(result).To(BeTrue())
			})
		})

		Context("with regular user", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "users", "developers"},
				}

				result := handler.isOrgAdminUser(user)

				Expect(result).To(BeFalse())
			})
		})

		Context("with no groups", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{},
				}

				result := handler.isOrgAdminUser(user)

				Expect(result).To(BeFalse())
			})
		})

		Context("with empty user", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{}

				result := handler.isOrgAdminUser(user)

				Expect(result).To(BeFalse())
			})
		})
	})

	Describe("isInternalUser", func() {
		BeforeEach(func() {
			handler = NewHandler(&config.Config{}, nil, nil, logger)
		})

		Context("with internal group", func() {
			It("should return true", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "internal", "users"},
				}

				result := handler.isInternalUser(user)

				Expect(result).To(BeTrue())
			})
		})

		Context("with group containing redhat", func() {
			It("should return true", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"redhat-employees", "users"},
				}

				result := handler.isInternalUser(user)

				Expect(result).To(BeTrue())
			})
		})

		Context("with external user", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{"team-lead", "users", "customers"},
				}

				result := handler.isInternalUser(user)

				Expect(result).To(BeFalse())
			})
		})

		Context("with no groups", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{
					Groups: []string{},
				}

				result := handler.isInternalUser(user)

				Expect(result).To(BeFalse())
			})
		})

		Context("with empty user", func() {
			It("should return false", func() {
				user := &authenticationv1.UserInfo{}

				result := handler.isInternalUser(user)

				Expect(result).To(BeFalse())
			})
		})
	})

	Context("Cluster Alias Logic", func() {
		var handler *Handler

		BeforeEach(func() {
			handler = &Handler{}
		})

		It("should use cluster alias when provided in manifest", func() {
			manifest := &Manifest{
				ClusterID:    "cluster-123",
				ClusterAlias: "production-cluster",
			}

			result := handler.getClusterAlias(manifest)

			Expect(result).To(Equal("production-cluster"))
		})

		It("should fallback to cluster ID when alias is empty", func() {
			manifest := &Manifest{
				ClusterID:    "cluster-456",
				ClusterAlias: "",
			}

			result := handler.getClusterAlias(manifest)

			Expect(result).To(Equal("cluster-456"))
		})

		It("should fallback to cluster ID when alias is not provided", func() {
			manifest := &Manifest{
				ClusterID: "cluster-789",
				// ClusterAlias not set (zero value)
			}

			result := handler.getClusterAlias(manifest)

			Expect(result).To(Equal("cluster-789"))
		})
	})
})
