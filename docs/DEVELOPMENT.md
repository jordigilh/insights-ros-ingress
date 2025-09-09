# Development Environment Setup

This document explains how to set up and use the development environment for insights-ros-ingress.

## Prerequisites

- [KIND (Kubernetes in Docker)](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Podman and podman-compose](https://podman.io/getting-started/installation)
- [Go 1.21+](https://golang.org/doc/install)

### Installation Commands

```bash
# macOS
brew install kind kubectl podman podman-compose

# Linux (example for Ubuntu/Debian)
# Follow official installation guides for your distribution
```

## Quick Start

1. **Start the complete development environment** (includes KIND cluster + services):
   ```bash
   make dev-env-up
   ```

2. **Run the application** with development configuration:
   ```bash
   make run-dev
   ```

3. **Stop the development environment** when done:
   ```bash
   make dev-env-down
   ```

## What `make dev-env-up` Does

The development environment setup creates:

1. **KIND Cluster**: A lightweight Kubernetes cluster named `insights-dev`
2. **Service Account**: Creates `insights-ros-ingress-dev` service account with proper permissions
3. **Authentication Token**: Generates a service account token for API authentication
4. **Development Services**: Starts MinIO, Kafka, Zookeeper, and PostgreSQL using podman-compose
5. **Configuration Files**: Updates `configs/local-dev.env` with the generated token

## Development Workflow

### Environment Components

- **KIND Cluster**: `insights-dev` cluster for Kubernetes API authentication
- **MinIO**: S3-compatible storage at `localhost:9000` (console: `localhost:9001`)
- **Kafka**: Message broker at `localhost:9092`
- **PostgreSQL**: Database at `localhost:5432`
- **Zookeeper**: Kafka coordination at `localhost:2181`

### Authentication Flow

The application uses Kubernetes TokenReviewer API for authentication:

1. KIND cluster provides the Kubernetes API server
2. Service account token validates incoming requests
3. KubernetesAuthMiddleware handles token validation
4. No fallback to insecure authentication (production-ready)

### Configuration Files

- `configs/local-dev.env`: Main development configuration
- `/tmp/dev-kubeconfig`: Kubernetes configuration for the KIND cluster
- `/tmp/dev-auth.env`: Generated authentication credentials

### Available Make Targets

```bash
# Development environment
make dev-env-up          # Start KIND cluster and all services
make dev-env-down        # Stop everything and cleanup

# Application
make run-dev             # Run with development configuration
make build               # Build the application
make test                # Run tests

# Verification
make verify-kafka        # Check Kafka topics and messages
make verify-minio        # Check MinIO buckets and uploads
```

## Manual Setup (Alternative)

If you need to set up components individually:

1. **Setup KIND cluster and authentication**:
   ```bash
   ./deployments/docker-compose/scripts/setup-dev-auth.sh
   ```

2. **Start services**:
   ```bash
   podman-compose -f deployments/docker-compose/docker-compose.yml up -d
   ```

3. **Source the configuration**:
   ```bash
   source configs/local-dev.env
   ```

4. **Run the application**:
   ```bash
   go run ./cmd/insights-ros-ingress
   ```

## Testing the Setup

### Verify Services

```bash
# Check KIND cluster
kubectl --kubeconfig=/tmp/dev-kubeconfig get nodes

# Check service account
kubectl --kubeconfig=/tmp/dev-kubeconfig get serviceaccounts insights-ros-ingress-dev

# Check services
podman-compose -f deployments/docker-compose/docker-compose.yml ps

# Check MinIO
curl http://localhost:9000/minio/health/live

# Check Kafka topics
make verify-kafka
```

### Test Authentication

```bash
# Test with a valid token (after sourcing configs/local-dev.env)
curl -X POST \
  -H "Content-Type: application/octet-stream" \
  -H "Authorization: Bearer $DEV_SERVICE_ACCOUNT_TOKEN" \
  -H "x-rh-identity: $(echo '{"identity":{"account_number":"12345","org_id":"12345","type":"User"}}' | base64)" \
  --data-binary "@test-file.tar.gz" \
  "http://localhost:8080/api/ingress/v1/upload?request_id=test-$(date +%s)"
```

## Troubleshooting

### Common Issues

1. **KIND cluster creation fails**:
   ```bash
   # Check if cluster exists
   kind get clusters
   
   # Delete and recreate
   kind delete cluster --name insights-dev
   make dev-env-up
   ```

2. **Service account token not working**:
   ```bash
   # Check token generation
   kubectl --kubeconfig=/tmp/dev-kubeconfig get secret insights-ros-ingress-dev-token -o yaml
   
   # Regenerate authentication
   ./deployments/docker-compose/scripts/setup-dev-auth.sh
   ```

3. **Services not starting**:
   ```bash
   # Check service logs
   podman-compose -f deployments/docker-compose/docker-compose.yml logs service-name
   
   # Restart specific service
   podman-compose -f deployments/docker-compose/docker-compose.yml restart service-name
   ```

4. **Port conflicts**:
   ```bash
   # Check what's using ports
   lsof -i :8080  # Application
   lsof -i :9000  # MinIO
   lsof -i :9092  # Kafka
   lsof -i :6443  # Kubernetes API
   ```

### Reset Everything

```bash
# Stop and remove everything
make dev-env-down

# Clean up any leftover resources
kind delete cluster --name insights-dev
podman system prune -f

# Start fresh
make dev-env-up
```

## Production Considerations

This development setup:

- ✅ Uses secure authentication (no NoOpAuthMiddleware)
- ✅ Validates tokens using Kubernetes API
- ✅ Requires proper authorization headers
- ✅ Fails securely if authentication is not available
