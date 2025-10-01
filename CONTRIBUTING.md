# Contributing to Insights ROS Ingress

Thank you for your interest in contributing to the Insights ROS Ingress project! This document provides guidelines and information for contributors.

## Development Environment

### Prerequisites

- Go 1.21 or later
- Podman 4.0 or later
- Make
- Helm 3.x (for chart development)

### Development Tools Installation

```bash
# Install development tools
make install-tools

# Or manually install:
# golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.54.2

# goimports
go install golang.org/x/tools/cmd/goimports@latest
```

### Setting up the Development Environment

1. Clone the repository:
```bash
git clone https://github.com/RedHatInsights/insights-ros-ingress.git
cd insights-ros-ingress
```

2. Start the development environment:
```bash
# Start MinIO, Kafka, and other dependencies
make dev-env-up

# In another terminal, build and run the service
make run
```

3. Stop the development environment:
```bash
make dev-env-down
```

## Code Quality

### Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports` for formatting
- Run `make fmt` to format your code
- Follow the existing code patterns in the project

### Linting

Run the linters before submitting code:

```bash
make lint
make vet
```

### Testing

Write tests for new functionality:

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# View coverage report
open coverage.html
```

### Building

```bash
# Build the binary
make build

# Build container image (using podman)
make build-image

# Build everything
make all
```

## Container Development

```bash
# Build image with podman
make build-image

# Push image to registry
make build-image-push
```

## Submitting Changes

### Pull Request Process

1. **Fork** the repository
2. **Create** a feature branch from `main`
3. **Make** your changes following the guidelines
4. **Test** your changes thoroughly
5. **Run** the full test suite and linting
6. **Submit** a pull request

### Pull Request Requirements

- [ ] All tests pass (`make test`)
- [ ] Code is properly formatted (`make fmt`)
- [ ] Linting passes (`make lint`)
- [ ] Changes are documented
- [ ] Commit messages are clear and descriptive
- [ ] No trailing whitespace (automatically checked)

### Commit Message Format

Use clear, descriptive commit messages:

```
component: brief description of the change

Longer description of what changed and why, if necessary.

Fixes #issue-number
```

Examples:
```
config: add MinIO SSL configuration support

Add support for SSL/TLS connections to MinIO storage backend.
This enables secure connections in production environments.

Fixes #123
```

```
upload: improve error handling for malformed payloads

Add better validation and error messages for corrupted or
invalid tar.gz uploads to help with debugging.
```

## Code Organization

### Project Structure

```
├── cmd/                       # Application entry points
├── internal/                  # Private application code
│   ├── config/               # Configuration management
│   ├── upload/               # Upload handling logic
│   ├── storage/              # MinIO storage client
│   ├── messaging/            # Kafka producer
│   ├── logger/               # Logging utilities
│   └── health/               # Health checks and metrics
├── deployments/              # Deployment configurations
│   ├── kubernetes/helm/      # Helm charts for Kubernetes/OpenShift
│   └── docker-compose/       # Docker Compose for development
├── docs/                     # Documentation
└── configs/                  # Configuration files
```

### Adding New Features

1. **Design**: Discuss large changes in an issue first
2. **Interface**: Define clear interfaces for new components
3. **Implementation**: Follow existing patterns
4. **Tests**: Add comprehensive tests
5. **Documentation**: Update relevant documentation
6. **Configuration**: Add necessary config options

### Error Handling

- Use structured errors with context
- Log errors at appropriate levels
- Provide meaningful error messages
- Handle graceful degradation where possible

### Metrics and Observability

- Add Prometheus metrics for new functionality
- Include relevant log statements with structured fields
- Ensure health checks cover new dependencies

## Deployment

### Helm Chart Development

When modifying the Helm chart:

```bash
# Lint the chart
make helm-lint

# Generate templates for review
make helm-template

# Package the chart
make helm-package
```

### OpenShift Deployment

Test deployments on OpenShift:

```bash
# Deploy to current OpenShift project
make oc-deploy

# Remove deployment
make oc-undeploy
```

## Security

### Security Considerations

- Never commit secrets or credentials
- Use secure defaults in configuration
- Validate all inputs thoroughly
- Follow security best practices for container images

### Reporting Security Issues

Please report security vulnerabilities privately to the maintainers.

## Getting Help

- **Issues**: Use GitHub issues for bugs and feature requests
- **Discussions**: Use GitHub discussions for questions
- **Documentation**: Check the docs/ directory
- **Code Examples**: Look at existing tests for usage examples

## License

By contributing to this project, you agree that your contributions will be licensed under the Apache License 2.0.