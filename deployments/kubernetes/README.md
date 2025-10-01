# Insights ROS Ingress Kubernetes Deployment

This directory contains Kubernetes deployment resources for the Insights ROS Ingress service, including Helm charts and deployment scripts for local development and testing.

## Overview

The Insights ROS Ingress service processes file uploads and validates them for the Red Hat Insights Resource Optimization Service (ROS). This Kubernetes deployment includes all necessary dependencies for a complete, functional environment.

## Components

### Main Service
- **insights-ros-ingress**: The main ingress service that handles file uploads, validation, and processing

### Dependencies
- **MinIO**: S3-compatible object storage for file storage
- **Kafka**: Message streaming platform for event processing
- **Zookeeper**: Coordination service required by Kafka

## Directory Structure

```
deployments/kubernetes/
├── scripts/
│   ├── deploy-kind.sh              # KIND cluster deployment script
│   ├── install-helm-chart.sh       # Helm chart installation (pulls from GitHub)
│   ├── test-k8s-dataflow.sh        # Kubernetes dataflow testing script
│   └── cleanup-kind-artifacts.sh   # Cleanup script for KIND
└── README.md                       # This file
```

**Note:** The Helm chart is maintained in a separate repository: [insights-onprem/ros-helm-chart](https://github.com/insights-onprem/ros-helm-chart)

## Quick Start

### Prerequisites

Install the following tools:
- [KIND](https://kind.sigs.k8s.io/) - Kubernetes in Docker
- [kubectl](https://kubernetes.io/docs/tasks/tools/) - Kubernetes CLI
- [Helm](https://helm.sh/) - Kubernetes package manager
- [Podman](https://podman.io/) - Container engine (recommended)

**macOS Installation:**
```bash
brew install kind kubectl helm podman
```

### Deploy to KIND

1. **Create and deploy to KIND cluster:**
   ```bash
   ./deployments/kubernetes/scripts/deploy-kind.sh
   ```
   
   This script will:
   - Create a KIND cluster if it doesn't exist
   - Install the Helm chart from GitHub (pulls latest release automatically)
   - Configure all necessary services

2. **Check deployment status:**
   ```bash
   ./deployments/kubernetes/scripts/install-helm-chart.sh status
   ```

3. **Run health checks:**
   ```bash
   ./deployments/kubernetes/scripts/install-helm-chart.sh health
   ```

4. **Test the complete dataflow:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh
   ```

### Access Points

After deployment, the following services are available:

- **Insights ROS Ingress API**: http://localhost:30080
  - Health: http://localhost:30080/health
  - Ready: http://localhost:30080/ready
  - Metrics: http://localhost:30080/metrics (requires auth)
  - Upload: http://localhost:30080/api/ingress/v1/upload

- **MinIO Console**: http://localhost:32061
  - Username: `minioadmin`
  - Password: `minioadmin123`

- **MinIO S3 API**: http://localhost:32061

## Configuration

### Helm Values

The Helm chart is pulled from GitHub. You can customize the deployment by providing a custom values file:

```bash
# Create a custom values file
cat > my-values.yaml <<EOF
image:
  tag: v1.2.3
replicaCount: 3
minio:
  persistence:
    size: 50Gi
EOF

# Deploy with custom values
VALUES_FILE=my-values.yaml ./deployments/kubernetes/scripts/install-helm-chart.sh
```

### Environment Variables

Key environment variables for the deployment script:

- `HELM_RELEASE_NAME`: Name of the Helm release (default: `ros-ocp`)
- `NAMESPACE`: Kubernetes namespace (default: `ros-ocp`)
- `VALUES_FILE`: Path to custom values file (optional)
- `USE_LOCAL_CHART`: Use local chart instead of GitHub release (default: `false`)
- `LOCAL_CHART_PATH`: Path to local chart directory (default: `../helm/ros-ocp`)

### Image Configuration

To use a custom image, create a values file:

```bash
cat > custom-image-values.yaml <<EOF
image:
  repository: your-registry/insights-ros-ingress
  tag: your-tag
EOF

VALUES_FILE=custom-image-values.yaml ./deployments/kubernetes/scripts/install-helm-chart.sh
```

## Development Workflow

### Local Testing

1. **Build and test locally:**
   ```bash
   # Build the application
   make build
   
   # Run integration tests
   make test-integration
   ```

2. **Deploy to KIND:**
   ```bash
   ./deployments/kubernetes/scripts/deploy-kind.sh
   ```

3. **Test the deployment:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh
   ```

4. **View logs:**
   ```bash
   kubectl logs -n insights-ros-ingress -l app.kubernetes.io/instance=insights-ros-ingress -f
   ```

### Updating the Deployment

1. **Update to latest Helm chart:**
   ```bash
   # The script automatically pulls the latest chart from GitHub
   ./deployments/kubernetes/scripts/install-helm-chart.sh
   ```

2. **Update with custom image version:**
   ```bash
   cat > update-values.yaml <<EOF
   image:
     tag: new-version
   EOF
   
   VALUES_FILE=update-values.yaml ./deployments/kubernetes/scripts/install-helm-chart.sh
   ```

3. **Rolling restart:**
   ```bash
   kubectl rollout restart deployment -n ros-ocp -l app.kubernetes.io/instance=ros-ocp
   ```

## Testing

### Automated Testing

The repository includes GitHub Actions workflows (see `.github/workflows/`) that automatically:

- Run unit and integration tests
- Build and push container images
- Deploy to KIND clusters for testing
- Run dataflow tests
- Perform security scans

### Manual Testing

1. **Health checks:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh health
   ```

2. **Upload API:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh upload
   ```

3. **Storage verification:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh storage
   ```

4. **Kafka verification:**
   ```bash
   ./deployments/kubernetes/scripts/test-k8s-dataflow.sh kafka
   ```

## Troubleshooting

### Common Issues

1. **Pods not starting:**
   ```bash
   kubectl get pods -n insights-ros-ingress
   kubectl describe pod <pod-name> -n insights-ros-ingress
   kubectl logs <pod-name> -n insights-ros-ingress
   ```

2. **Storage issues:**
   ```bash
   kubectl get pvc -n insights-ros-ingress
   kubectl get storageclass
   ```

3. **Network connectivity:**
   ```bash
   kubectl get services -n insights-ros-ingress
   kubectl port-forward -n insights-ros-ingress svc/insights-ros-ingress 8080:8080
   ```

### Debug Commands

```bash
# Check all resources
kubectl get all -n insights-ros-ingress

# View events
kubectl get events -n insights-ros-ingress --sort-by='.lastTimestamp'

# Check Helm release
helm status insights-ros-ingress -n insights-ros-ingress

# View Helm values
helm get values insights-ros-ingress -n insights-ros-ingress
```

## Cleanup

### Remove Deployment

```bash
# Remove Helm release only (preserves PVs)
./deployments/kubernetes/scripts/install-helm-chart.sh cleanup

# Complete cleanup including Persistent Volumes
./deployments/kubernetes/scripts/install-helm-chart.sh cleanup --complete

# Remove entire KIND cluster
kind delete cluster --name ros-ocp-cluster
```

### Manual Cleanup

```bash
# Uninstall Helm release
helm uninstall ros-ocp -n ros-ocp

# Delete namespace
kubectl delete namespace ros-ocp

# Delete KIND cluster
kind delete cluster --name ros-ocp-cluster
```

## Production Considerations

When deploying to production environments:

1. **Security**: 
   - Use proper authentication and authorization
   - Enable TLS for all communications
   - Use Kubernetes secrets for sensitive data

2. **Persistence**:
   - Use appropriate storage classes for production workloads
   - Configure backup strategies for persistent data

3. **Monitoring**:
   - Enable ServiceMonitor for Prometheus
   - Configure proper logging aggregation
   - Set up alerting for critical metrics

4. **Scaling**:
   - Adjust replica counts based on load
   - Configure horizontal pod autoscaling
   - Size persistent volumes appropriately

5. **Updates**:
   - Use rolling updates for zero-downtime deployments
   - Test updates in staging environments first
   - Have rollback procedures ready

## Contributing

When contributing to the Kubernetes deployment:

1. Test changes locally using KIND
2. Update documentation for any configuration changes
3. Ensure GitHub Actions tests pass
4. Follow Helm best practices for chart development

## Support

For issues and questions:
- Check the troubleshooting section above
- Review logs for error messages
- Open an issue in the project repository