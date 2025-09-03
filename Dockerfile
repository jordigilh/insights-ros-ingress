# Multi-stage Dockerfile optimized for podman builds
# Based on insights-ingress-go patterns with security enhancements

# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:1.21 AS builder

# Set working directory
WORKDIR /workspace

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build the application
# Using static linking and security flags
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags='-w -s -extldflags "-static"' \
    -o insights-ros-ingress \
    ./cmd/insights-ros-ingress

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Install ca-certificates for TLS connections
RUN microdnf update -y && \
    microdnf install -y ca-certificates tzdata && \
    microdnf clean all && \
    rm -rf /var/cache/yum

# Create non-root user for security
RUN groupadd -r appuser && \
    useradd -r -g appuser -u 1001 -m -d /home/appuser -s /sbin/nologin appuser

# Set up directories with proper ownership
RUN mkdir -p /app/data /app/logs && \
    chown -R appuser:appuser /app

# Copy the binary from builder stage
COPY --from=builder /workspace/insights-ros-ingress /app/insights-ros-ingress

# Make binary executable
RUN chmod +x /app/insights-ros-ingress

# Switch to non-root user
USER appuser

# Set working directory
WORKDIR /app

# Set up environment
ENV PATH="/app:${PATH}"
ENV APP_ENV=production

# Expose the application port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Set entrypoint
ENTRYPOINT ["/app/insights-ros-ingress"]

# Labels for metadata (following OpenShift/OCI standards)
LABEL name="insights-ros-ingress" \
      version="1.0.0" \
      description="Insights ROS Ingress service for processing HCCM uploads" \
      summary="Specialized ingress service for ROS data extraction" \
      maintainer="Red Hat Insights <insights-dev@redhat.com>" \
      vendor="Red Hat" \
      licenses="Apache-2.0" \
      io.k8s.description="Insights ROS Ingress service for processing HCCM uploads" \
      io.k8s.display-name="Insights ROS Ingress" \
      io.openshift.tags="insights,ros,ingress,openshift"