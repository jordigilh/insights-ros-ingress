# GitHub Actions Workflows

This directory contains GitHub Actions workflows for the insights-ros-ingress project.

## Workflows

### 🧪 test.yml
**Trigger**: Push/PR to main/develop branches
**Purpose**: Fast feedback unit testing
**Duration**: ~5-10 minutes

- Runs unit tests with race detection
- Performs static analysis (go vet)
- Runs code linting (golangci-lint)
- Generates test coverage reports
- Verifies binary builds

### 🐳 docker-compose-test.yml
**Trigger**: Push/PR affecting deployments or core code
**Purpose**: Full integration testing with docker-compose
**Duration**: ~20-25 minutes

- Sets up Podman and required services
- Starts MinIO, Kafka, Zookeeper via docker-compose
- Runs complete end-to-end integration tests
- Validates ROS data processing pipeline
- Tests upload API and Kafka messaging

### 🚀 build-and-push.yml
**Trigger**: Push to main, merged PRs, manual dispatch
**Purpose**: Build and publish container images
**Duration**: ~10-15 minutes

- Runs tests and linting
- Builds multi-platform container images (amd64/arm64)
- Pushes to quay.io/insights-onprem/insights-ros-ingress
- Supports custom tagging via workflow dispatch

## Container Registry

Images are published to: `quay.io/insights-onprem/insights-ros-ingress`

### Available Tags
- `latest` - Latest main branch build
- `main-<sha>` - Specific commit from main
- `pr-<number>` - Pull request builds
- Custom tags via manual workflow dispatch

## Required Secrets

Configure these secrets in GitHub repository settings:

- `QUAY_USERNAME` - Quay.io username for image pushes
- `QUAY_PASSWORD` - Quay.io password/token for image pushes

## Workflow Dependencies

### System Dependencies (auto-installed)
- Go 1.21
- Podman and podman-compose
- Docker Buildx
- golangci-lint
- curl, netcat-openbsd

### Make Targets Used
- `make test` - Unit tests
- `make test-coverage` - Coverage reports
- `make lint` - Code linting
- `make vet` - Static analysis
- `make build` - Binary compilation
- `make test-integration` - Full integration test

## Performance Considerations

- **Caching**: Go modules are cached between runs
- **Parallelization**: Multi-platform builds use cache
- **Timeouts**: Each workflow has appropriate timeouts
- **Cleanup**: Docker-compose workflow cleans up resources

## Monitoring & Debugging

### Workflow Status
- Check GitHub Actions tab for build status
- Each workflow generates summary reports
- Failed runs include detailed logs

### Local Testing
```bash
# Run the same tests locally
make test
make test-integration

# Build images locally
make image
```

### Common Issues
1. **Podman setup failures**: Check Ubuntu version compatibility
2. **Timeout issues**: Services may need more startup time
3. **Registry push failures**: Verify QUAY_* secrets are set
4. **Test data issues**: Ensure test-data directory is created

## Contributing

When adding new workflows:
1. Follow existing naming conventions
2. Include appropriate timeouts
3. Add cleanup steps with `if: always()`
4. Generate step summaries for visibility
5. Test locally when possible
6. Update this README with new workflow details