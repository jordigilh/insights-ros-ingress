#!/bin/bash
# Setup development authentication for insights-ros-ingress
# Creates service account and token for KubernetesAuthMiddleware

set -e

CLUSTER_NAME="insights-dev"
NAMESPACE="default"
SERVICE_ACCOUNT="insights-ros-ingress-dev"

echo "Setting up development authentication..."

# Check if KIND is installed
if ! command -v kind >/dev/null 2>&1; then
    echo "ERROR: KIND is not installed. Please install KIND first:"
    echo "  # On macOS:"
    echo "  brew install kind"
    echo "  # Or download from: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
    exit 1
fi

# Check if kubectl is installed
if ! command -v kubectl >/dev/null 2>&1; then
    echo "ERROR: kubectl is not installed. Please install kubectl first:"
    echo "  # On macOS:"
    echo "  brew install kubectl"
    exit 1
fi

# Create KIND cluster if it doesn't exist
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo "Creating KIND cluster: $CLUSTER_NAME"
    cat <<EOF | kind create cluster --name="$CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 6443
    hostPort: 6443
    protocol: TCP
EOF
else
    echo "KIND cluster '$CLUSTER_NAME' already exists"
fi

# Set kubeconfig context
kubectl config use-context "kind-${CLUSTER_NAME}"

# Configure cluster to skip TLS verification for development
kubectl config set clusters.kind-${CLUSTER_NAME}.insecure-skip-tls-verify true
kubectl config unset clusters.kind-${CLUSTER_NAME}.certificate-authority-data

# Wait for API server to be ready
echo "Waiting for Kubernetes API server to be ready..."
for i in {1..30}; do
    if kubectl get nodes >/dev/null 2>&1; then
        echo "Kubernetes API server is ready"
        break
    fi
    echo "Waiting for API server... ($i/30)"
    sleep 2
done

# Create service account
echo "Creating service account: $SERVICE_ACCOUNT"
kubectl create serviceaccount "$SERVICE_ACCOUNT" -n "$NAMESPACE" --dry-run=client -o yaml | \
kubectl apply --validate=false -f -

# Create ClusterRoleBinding for system:auth-delegator (required for TokenReviewer API)
echo "Creating ClusterRoleBinding for system:auth-delegator"
cat <<EOF | kubectl apply --validate=false -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${SERVICE_ACCOUNT}-token-reviewer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
- kind: ServiceAccount
  name: $SERVICE_ACCOUNT
  namespace: $NAMESPACE
EOF

# Create a long-lived token secret for the service account
echo "Creating token secret for service account"
cat <<EOF | kubectl apply --validate=false -f -
apiVersion: v1
kind: Secret
metadata:
  name: ${SERVICE_ACCOUNT}-token
  namespace: $NAMESPACE
  annotations:
    kubernetes.io/service-account.name: $SERVICE_ACCOUNT
type: kubernetes.io/service-account-token
EOF

# Wait for the token to be generated
echo "Waiting for token to be generated..."
for i in {1..30}; do
    if kubectl get secret "${SERVICE_ACCOUNT}-token" -n "$NAMESPACE" -o jsonpath='{.data.token}' >/dev/null 2>&1; then
        break
    fi
    echo "Waiting for token generation... ($i/30)"
    sleep 2
done

# Get the token
TOKEN=$(kubectl get secret "${SERVICE_ACCOUNT}-token" -n "$NAMESPACE" -o jsonpath='{.data.token}' | base64 -d)

if [ -z "$TOKEN" ]; then
    echo "ERROR: Failed to get service account token"
    exit 1
fi

echo "Service account token generated successfully"

# Save the token and kubeconfig for the application
echo "Saving development configuration..."

# Create a kubeconfig for the application
KIND_CLUSTER_NAME="kind-${CLUSTER_NAME}"
CLUSTER_SERVER=$(kubectl config view --raw -o jsonpath="{.clusters[?(@.name=='${KIND_CLUSTER_NAME}')].cluster.server}")

# Create kubeconfig for the application
cat > /tmp/dev-kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: $CLUSTER_SERVER
    insecure-skip-tls-verify: true
  name: kind-dev
contexts:
- context:
    cluster: kind-dev
    user: $SERVICE_ACCOUNT
  name: kind-dev
current-context: kind-dev
users:
- name: $SERVICE_ACCOUNT
  user:
    token: $TOKEN
EOF

echo "Development kubeconfig created at /tmp/dev-kubeconfig"

# Create environment file for the application
cat > /tmp/dev-auth.env <<EOF
# Development authentication configuration
KUBECONFIG=/tmp/dev-kubeconfig
DEV_SERVICE_ACCOUNT_TOKEN=$TOKEN
EOF

# Update the local development config with the token
CONFIG_FILE="$(pwd)/configs/local-dev.env"
if [ -f "$CONFIG_FILE" ]; then
    # Update or add the token line
    if grep -q "^DEV_SERVICE_ACCOUNT_TOKEN=" "$CONFIG_FILE"; then
        sed -i.bak "s/^DEV_SERVICE_ACCOUNT_TOKEN=.*/DEV_SERVICE_ACCOUNT_TOKEN=$TOKEN/" "$CONFIG_FILE"
    else
        echo "DEV_SERVICE_ACCOUNT_TOKEN=$TOKEN" >> "$CONFIG_FILE"
    fi
    echo "Updated development configuration: $CONFIG_FILE"
fi

echo "Development authentication setup completed successfully!"
echo ""
echo "To use this configuration, you can:"
echo "1. Source the generated environment file: source /tmp/dev-auth.env"
echo "2. Use the updated local config: source configs/local-dev.env"
echo "3. Run with make: make run-dev (uses configs/local-dev.env)"
echo ""
echo "Environment variables set:"
echo "  KUBECONFIG=/tmp/dev-kubeconfig"
echo "  DEV_SERVICE_ACCOUNT_TOKEN=$TOKEN"