# Deployment Example

This document provides examples of how to deploy the Insights ROS Ingress service in different environments.

## OpenShift Deployment

### Prerequisites

- OpenShift cluster with admin access
- Helm 3.x installed
- MinIO and Kafka services available

### Step 1: Create Project

```bash
oc new-project insights-ros
```

### Step 2: Create Required Secrets

```bash
# Storage credentials for MinIO
oc create secret generic minio-credentials \
  --from-literal=access-key=minioadmin \
  --from-literal=secret-key=minioadmin123

# Authentication secret (if auth is enabled)
oc create secret generic auth-credentials \
  --from-literal=jwt-secret=your-jwt-secret-here

# Kafka credentials (if using SASL)
oc create secret generic kafka-credentials \
  --from-literal=username=kafka-user \
  --from-literal=password=kafka-password
```

### Step 3: Create Values Override

```yaml
# values-production.yaml
replicaCount: 3

image:
  repository: quay.io/redhat-insights/insights-ros-ingress
  tag: "1.0.0"

storage:
  endpoint: "minio.storage.svc.cluster.local:9000"
  bucket: "insights-ros-data"
  useSSL: false

kafka:
  brokers:
    - "kafka-broker-1.kafka.svc.cluster.local:9092"
    - "kafka-broker-2.kafka.svc.cluster.local:9092"
  topic: "hccm.ros.events"

route:
  enabled: true
  host: "insights-ros-ingress.apps.your-cluster.com"
  tls:
    enabled: true
    termination: edge

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

### Step 4: Deploy

```bash
# Deploy with Helm
helm upgrade --install insights-ros-ingress \
  ./deployments/helm/insights-ros-ingress \
  --values values-production.yaml \
  --namespace insights-ros
```

### Step 5: Verify Deployment

```bash
# Check pod status
oc get pods -l app.kubernetes.io/name=insights-ros-ingress

# Check route
oc get route insights-ros-ingress

# Check logs
oc logs -l app.kubernetes.io/name=insights-ros-ingress -f

# Test health endpoint
curl -k https://$(oc get route insights-ros-ingress -o jsonpath='{.spec.host}')/health
```

## Development Deployment

### Using Podman Compose

```bash
# Start development environment
make dev-env-up

# In another terminal, run the service
export STORAGE_ENDPOINT=localhost:9000
export STORAGE_ACCESS_KEY=minioadmin
export STORAGE_SECRET_KEY=minioadmin123
export KAFKA_BROKERS=localhost:9092
export AUTH_ENABLED=false

make run
```

### Using Kind (Kubernetes in Docker)

```bash
# Create Kind cluster
kind create cluster --name insights-ros

# Load image into Kind
make image
kind load docker-image insights-ros-ingress:latest --name insights-ros

# Deploy with Helm
helm upgrade --install insights-ros-ingress \
  ./deployments/helm/insights-ros-ingress \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --create-namespace \
  --namespace insights-ros
```

## Configuration Examples

### MinIO with SSL

```yaml
storage:
  endpoint: "minio.storage.svc.cluster.local:9000"
  useSSL: true
  bucket: "insights-ros-data"
  existingSecret: "minio-tls-credentials"
```

### Kafka with SASL/SSL

```yaml
kafka:
  brokers:
    - "kafka.messaging.svc.cluster.local:9093"
  securityProtocol: "SASL_SSL"
  security:
    enabled: true
    saslMechanism: "SCRAM-SHA-512"
    existingSecret: "kafka-sasl-credentials"
    sslCaLocation: "/etc/ssl/certs/ca-bundle.crt"
```

### Resource Limits for Different Environments

#### Development
```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

#### Production
```yaml
resources:
  limits:
    cpu: 2000m
    memory: 2Gi
  requests:
    cpu: 500m
    memory: 512Mi
```

### High Availability Configuration

```yaml
replicaCount: 5

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - insights-ros-ingress
        topologyKey: kubernetes.io/hostname

podDisruptionBudget:
  enabled: true
  minAvailable: 2
```

## Testing the Deployment

### Upload Test File

```bash
# Create test HCCM payload
cat > test-manifest.json << EOF
{
  "uuid": "test-12345",
  "cluster_id": "test-cluster",
  "date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "files": ["usage.csv"],
  "resource_optimization_files": ["ros-data.csv"]
}
EOF

# Create test files and tar.gz
echo "node,cpu_request,memory_request" > ros-data.csv
echo "node1,100m,256Mi" >> ros-data.csv
tar -czf test-payload.tar.gz test-manifest.json ros-data.csv

# Upload via curl
curl -X POST \
  -H "Content-Type: application/vnd.redhat.hccm.upload" \
  -F "file=@test-payload.tar.gz" \
  https://your-route-host/api/ingress/v1/upload
```

### Monitor Metrics

```bash
# Port forward to access metrics
oc port-forward svc/insights-ros-ingress 8080:8080

# Query metrics
curl http://localhost:8080/metrics | grep insights_ros
```

## Troubleshooting

### Common Issues

1. **Pod fails to start**: Check resource limits and node capacity
2. **Storage connection failed**: Verify MinIO credentials and network connectivity
3. **Kafka connection failed**: Check broker addresses and security configuration
4. **Upload fails**: Verify content-type and payload format

### Debug Commands

```bash
# Check pod events
oc describe pod -l app.kubernetes.io/name=insights-ros-ingress

# Check service endpoints
oc get endpoints insights-ros-ingress

# View configuration
oc get configmap insights-ros-ingress -o yaml

# Check secrets
oc get secret minio-credentials -o yaml
```