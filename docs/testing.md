# Testing Guide for insights-ros-ingress

This document provides comprehensive testing instructions for the insights-ros-ingress service, including integration tests with docker-compose services.

## Overview

The testing setup validates the complete end-to-end flow:
1. **Docker-compose services**: MinIO, Kafka, Zookeeper
2. **Local service**: insights-ros-ingress running with test configuration
3. **Upload API**: Accepts HCCM payloads with ROS data
4. **Processing**: Extracts ROS files and uploads to MinIO
5. **Messaging**: Sends Kafka events to ROS topic

## Quick Start

### 1. Run Full Integration Test
```bash
# Complete end-to-end test (starts services, tests, and cleans up)
make test-integration
```

### 2. Manual Testing with Running Services
```bash
# Start docker-compose services
make dev-env-up

# Wait for services to be ready (30-60 seconds)
sleep 30

# Run the service with test configuration
make run-test

# In another terminal, test the upload API
curl -X POST \
  -H "Content-Type: multipart/form-data" \
  -H "x-rh-identity: $(echo '{"identity":{"account_number":"12345","org_id":"12345","type":"User"}}' | base64 -w 0)" \
  -F "upload=@deployments/docker-compose/test-data/test-payload.tar.gz" \
  "http://localhost:8080/api/ingress/v1/upload?request_id=test-$(date +%s)"

# Verify results
make verify-kafka    # Check Kafka messages
make verify-minio    # Check MinIO uploads
```

### 3. Quick Test (Services Already Running)
```bash
# Assumes docker-compose services are already running
make test-integration-quick
```

## Test Components

### Test Data

Test payloads are created with realistic ROS data:

```bash
# Create test data (done automatically by integration test)
make test-data
```

This creates:
- **test-payload.tar.gz**: Full payload with multiple ROS CSV files
- **simple-payload.tar.gz**: Minimal payload for quick tests
- **invalid-payload.tar.gz**: Payload without ROS files (for error testing)

#### Test Data Contents

**manifest.json**:
```json
{
  "version": "1.0",
  "cluster_id": "test-cluster-123",
  "resource_optimization_files": [
    "cost-management.csv",
    "workload-optimization.csv",
    "node-optimization.csv"
  ]
}
```

**ROS CSV Files**:
- `cost-management.csv`: Pod-level cost and resource data
- `workload-optimization.csv`: Workload optimization recommendations
- `node-optimization.csv`: Node-level utilization and recommendations

### Configuration

The service uses test configuration from `configs/local-test.env`:
- **Storage**: MinIO at localhost:9000
- **Kafka**: localhost:9092
- **Authentication**: Disabled for testing
- **Logging**: Debug level

### Docker-Compose Services

Services defined in `deployments/docker-compose/docker-compose.yml`:

- **MinIO** (S3-compatible storage)
  - Console: http://localhost:9001
  - API: localhost:9000
  - Credentials: minioadmin/minioadmin123

- **Kafka** (Message broker)
  - Bootstrap servers: localhost:9092
  - Topics: `hccm.ros.events`, `platform.upload.validation`

- **Zookeeper** (Kafka coordination)
  - Port: 2181

## Verification Scripts

### Kafka Verification

```bash
# Verify Kafka setup and consume messages
make verify-kafka

# Monitor topics in real-time
make monitor-kafka

# Individual topic consumption
./deployments/docker-compose/verify-kafka.sh ros          # ROS events topic
./deployments/docker-compose/verify-kafka.sh validation   # Validation topic
```

### MinIO Verification

```bash
# Verify MinIO setup and ROS data
make verify-minio

# List bucket contents
./deployments/docker-compose/verify-minio.sh list

# Search for files by request ID
./deployments/docker-compose/verify-minio.sh search test-12345

# Examine a specific file
./deployments/docker-compose/verify-minio.sh examine ros/cost-management.csv
```

## Test Scenarios

### 1. Successful Upload

**Expected Flow**:
1. Upload tar.gz with ROS files
2. Service extracts manifest.json
3. Identifies ROS files from `resource_optimization_files`
4. Uploads each ROS file to MinIO with metadata
5. Sends Kafka message with file list and metadata

**Verification**:
- HTTP 200/202 response from upload API
- ROS files appear in MinIO bucket
- Kafka message contains request_id and file lists
- MinIO files have correct metadata

### 2. Invalid Payload

**Test with invalid payload**:
```bash
curl -X POST \
  -H "Content-Type: multipart/form-data" \
  -H "x-rh-identity: $(echo '{"identity":{"account_number":"12345","org_id":"12345","type":"User"}}' | base64 -w 0)" \
  -F "upload=@deployments/docker-compose/test-data/invalid-payload.tar.gz" \
  "http://localhost:8080/api/ingress/v1/upload?request_id=invalid-test"
```

**Expected**: HTTP 400 error, validation message sent to Kafka

### 3. Large Payload

**Test with larger payload**:
```bash
# Create larger test data
dd if=/dev/zero of=/tmp/large-file.csv bs=1M count=10
# Add to tar.gz and test upload
```

### 4. Authentication Testing

**Test without identity header**:
```bash
curl -X POST \
  -H "Content-Type: multipart/form-data" \
  -F "upload=@deployments/docker-compose/test-data/test-payload.tar.gz" \
  "http://localhost:8080/api/ingress/v1/upload"
```

**Expected**: HTTP 401 if auth enabled, or success if auth disabled

## Health Checks

### Service Health

```bash
# Health endpoint
curl http://localhost:8080/health

# Readiness endpoint
curl http://localhost:8080/ready

# Metrics endpoint
curl http://localhost:8080/metrics
```

### Infrastructure Health

```bash
# Check all services
podman-compose -f deployments/docker-compose/docker-compose.yml ps

# Check service logs
podman-compose -f deployments/docker-compose/docker-compose.yml logs minio
podman-compose -f deployments/docker-compose/docker-compose.yml logs kafka
```

## Troubleshooting

### Common Issues

**1. Services not starting**
```bash
# Check container status
podman ps -a

# Check logs
podman-compose -f deployments/docker-compose/docker-compose.yml logs

# Restart services
make dev-env-down && make dev-env-up
```

**2. Kafka topics not created**
```bash
# List topics
podman exec insights-ros-kafka bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list

# Create topics manually
podman exec insights-ros-kafka bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 \
  --create --topic hccm.ros.events --partitions 1 --replication-factor 1
```

**3. MinIO bucket not accessible**
```bash
# Check MinIO status
curl http://localhost:9000/minio/health/live

# Access MinIO console
open http://localhost:9001
```

**4. Service connection issues**
```bash
# Check if ports are in use
netstat -an | grep ':8080\|:9000\|:9092'

# Test connectivity
nc -zv localhost 9000
nc -zv localhost 9092
```

### Debug Mode

**Run service with debug logging**:
```bash
export LOG_LEVEL=debug
make run-test
```

**Enable verbose Kafka logging**:
```bash
export KAFKA_DEBUG=true
make run-test
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Integration Tests
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup Go
        uses: actions/setup-go@v3
        with:
          go-version: 1.24.4
      - name: Install Podman
        run: |
          sudo apt-get update
          sudo apt-get install -y podman podman-compose
      - name: Run Integration Tests
        run: make test-integration
```

### Test Reports

The integration test generates:
- **Exit code**: 0 for success, non-zero for failure
- **Logs**: Detailed output with timestamps and colors
- **Verification**: Automatic checks for expected files and messages

## Performance Testing

### Load Testing with curl

```bash
# Generate multiple requests
for i in {1..10}; do
  curl -X POST \
    -H "Content-Type: multipart/form-data" \
    -H "x-rh-identity: $(echo '{"identity":{"account_number":"12345","org_id":"12345","type":"User"}}' | base64 -w 0)" \
    -F "upload=@deployments/docker-compose/test-data/test-payload.tar.gz" \
    "http://localhost:8080/api/ingress/v1/upload?request_id=load-test-$i" &
done
wait
```

### Memory and CPU Monitoring

```bash
# Monitor service resources
podman stats insights-ros-ingress

# Monitor docker-compose services
podman-compose -f deployments/docker-compose/docker-compose.yml top
```

This testing framework provides comprehensive coverage of the insights-ros-ingress service functionality and integration points.