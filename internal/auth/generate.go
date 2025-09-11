package auth

//go:generate mockgen -destination=mocks/mock_k8s_auth.go -package=mocks k8s.io/client-go/kubernetes/typed/authentication/v1 AuthenticationV1Interface,TokenReviewInterface
