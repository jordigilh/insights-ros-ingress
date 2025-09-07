#!/bin/bash

# Integration test script for insights-ros-ingress
# Tests the complete flow: docker-compose services + local service + upload API
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
SERVICE_PORT=8080
KAFKA_TOPIC="hccm.ros.events"
VALIDATION_TOPIC="platform.upload.validation"

# Test configuration
TEST_DATA_DIR="$SCRIPT_DIR/test-data"
TEST_PAYLOAD="$TEST_DATA_DIR/test-payload.tar.gz"
TEST_REQUEST_ID="test-$(date +%s)"
TEST_ORG_ID="12345"

# Function to print colored output
log() {
    local color=$1
    shift
    echo -e "${color}[$(date '+%Y-%m-%d %H:%M:%S')] $*${NC}"
}

log_info() { log "$BLUE" "$@"; }
log_success() { log "$GREEN" "$@"; }
log_warning() { log "$YELLOW" "$@"; }
log_error() { log "$RED" "$@"; }

# Function to check if command exists
check_command() {
    if ! command -v "$1" &> /dev/null; then
        log_error "Command '$1' not found. Please install it first."
        exit 1
    fi
}

# Function to wait for service to be ready
wait_for_service() {
    local host=$1
    local port=$2
    local service_name=$3
    local timeout=${4:-60}

    log_info "Waiting for $service_name to be ready at $host:$port..."

    local count=0
    while [ $count -lt $timeout ]; do
        if nc -z "$host" "$port" 2>/dev/null; then
            log_success "$service_name is ready!"
            return 0
        fi
        sleep 1
        ((count++))
    done

    log_error "$service_name failed to start within ${timeout} seconds"
    return 1
}

# Function to wait for HTTP service
wait_for_http() {
    local url=$1
    local service_name=$2
    local timeout=${3:-60}

    log_info "Waiting for $service_name HTTP service at $url..."

    local count=0
    while [ $count -lt $timeout ]; do
        if curl -f -s "$url" >/dev/null 2>&1; then
            log_success "$service_name HTTP service is ready!"
            return 0
        fi
        sleep 1
        ((count++))
    done

    log_error "$service_name HTTP service failed to start within ${timeout} seconds"
    return 1
}

# Function to create test data
create_test_data() {
    log_info "Creating test data..."

    mkdir -p "$TEST_DATA_DIR"

    # Create a temporary directory for test files
    local temp_dir=$(mktemp -d)

    # Create manifest.json with ROS files
    cat > "$temp_dir/manifest.json" << EOF
{
    "uuid": "test-uuid-123e4567-e89b-12d3-a456-426614174000",
    "version": "1.0",
    "cluster_id": "test-cluster-123",
    "cluster_alias": "test-cluster",
    "date": "2024-01-01T00:00:00Z",
    "source_metadata": {
        "any": {
            "cluster_version": "4.14.0",
            "cluster_id": "test-cluster-123"
        }
    },
    "resource_optimization_files": [
        "cost-management.csv",
        "workload-optimization.csv"
    ],
    "files": [
        "manifest.json",
        "cost-management.csv",
        "workload-optimization.csv",
        "other-file.txt"
    ]
}
EOF

    # Create sample ROS CSV files
    cat > "$temp_dir/cost-management.csv" << EOF
date,namespace,pod,cpu_request,cpu_usage,memory_request,memory_usage,cost
2024-01-01,default,test-pod-1,100m,50m,128Mi,64Mi,1.50
2024-01-01,kube-system,dns-pod,200m,150m,256Mi,200Mi,3.20
2024-01-01,monitoring,prometheus,500m,400m,1Gi,800Mi,8.75
EOF

    cat > "$temp_dir/workload-optimization.csv" << EOF
namespace,workload,type,current_replicas,recommended_replicas,cpu_request,recommended_cpu,memory_request,recommended_memory
default,web-app,Deployment,3,2,200m,150m,512Mi,384Mi
kube-system,coredns,Deployment,2,2,100m,100m,70Mi,70Mi
monitoring,grafana,Deployment,1,1,100m,80m,128Mi,96Mi
EOF

    # Create other files
    echo "This is a non-ROS file for testing" > "$temp_dir/other-file.txt"

    # Create tar.gz archive
    (cd "$temp_dir" && tar -czf "$TEST_PAYLOAD" .)

    # Cleanup temp directory
    rm -rf "$temp_dir"

    log_success "Test payload created: $TEST_PAYLOAD"
}

# Function to start docker-compose services
start_services() {
    log_info "Starting docker-compose services..."

    # Clean up any existing volumes to avoid cluster ID conflicts
    podman-compose -f "$COMPOSE_FILE" down -v &>/dev/null || true
    
    # Start services in detached mode
    podman-compose -f "$COMPOSE_FILE" up -d

    # Wait for services to be ready
    wait_for_service localhost 9000 "MinIO"
    wait_for_service localhost 2181 "Zookeeper"
    wait_for_service localhost 9092 "Kafka"

    # Wait for Kafka topics to be created
    log_info "Waiting for Kafka topics to be created..."
    sleep 15

    # Verify and create topics if needed
    log_info "Ensuring Kafka topics exist..."
    podman exec insights-ros-kafka bin/kafka-topics.sh --create --if-not-exists --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1 --topic hccm.ros.events 2>/dev/null || true
    podman exec insights-ros-kafka bin/kafka-topics.sh --create --if-not-exists --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1 --topic platform.upload.validation 2>/dev/null || true
}

# Function to stop docker-compose services
stop_services() {
    log_info "Stopping docker-compose services..."
    podman-compose -f "$COMPOSE_FILE" down -v
}

# Function to start the insights-ros-ingress service
start_ingress_service() {
    log_info "Starting insights-ros-ingress service..."

    # Set environment variables for the service
    export LOG_LEVEL=debug
    export SERVER_PORT=$SERVICE_PORT
    export STORAGE_ENDPOINT=localhost:9000
    export STORAGE_ACCESS_KEY=minioadmin
    export STORAGE_SECRET_KEY=minioadmin123
    export STORAGE_BUCKET=insights-ros-data
    export STORAGE_USE_SSL=false
    export KAFKA_BROKERS=localhost:9092
    export KAFKA_TOPIC=$KAFKA_TOPIC
    export KAFKA_CLIENT_ID=insights-ros-ingress-test
    export KAFKA_SECURITY_PROTOCOL=PLAINTEXT
    export AUTH_ENABLED=false

    # Build and run the service in background
    make build
    ./build/bin/insights-ros-ingress &
    SERVICE_PID=$!

    # Wait for service to be ready
    wait_for_http "http://localhost:$SERVICE_PORT/health" "insights-ros-ingress"

    log_success "insights-ros-ingress service started with PID $SERVICE_PID"
    
    # Give the service extra time to establish Kafka connections
    log_info "Allowing time for Kafka connections to stabilize..."
    sleep 5
}

# Function to stop the ingress service
stop_ingress_service() {
    if [ -n "${SERVICE_PID:-}" ]; then
        log_info "Stopping insights-ros-ingress service (PID: $SERVICE_PID)..."
        kill $SERVICE_PID 2>/dev/null || true
        wait $SERVICE_PID 2>/dev/null || true
        log_success "Service stopped"
    fi
}

# Function to test the upload API
test_upload_api() {
    log_info "Testing upload API..."

    # Create identity header (base64 encoded)
    local identity=$(echo -n "{\"identity\":{\"account_number\":\"12345\",\"org_id\":\"$TEST_ORG_ID\",\"type\":\"User\",\"user\":{\"username\":\"testuser\",\"email\":\"test@example.com\"}}}" | base64 -w 0)

    # Upload the test payload
    local response=$(curl -s -w "\n%{http_code}" \
        -X POST \
        -H "x-rh-identity: $identity" \
        -F "upload=@$TEST_PAYLOAD;type=application/vnd.redhat.hccm.upload" \
        "http://localhost:$SERVICE_PORT/api/ingress/v1/upload?request_id=$TEST_REQUEST_ID")

    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | head -n -1)

    log_info "Upload response (HTTP $http_code): $body"

    if [ "$http_code" = "200" ] || [ "$http_code" = "202" ]; then
        log_success "Upload API test passed!"
        return 0
    else
        log_error "Upload API test failed with HTTP $http_code"
        return 1
    fi
}

# Function to verify MinIO bucket contents
verify_minio_upload() {
    log_info "Verifying MinIO bucket contents..."

    # Wait a moment for files to be fully written
    sleep 3

    # Set up MinIO alias (the setup container doesn't persist aliases to the main container)
    if ! podman exec insights-ros-minio mc alias set myminio http://localhost:9000 minioadmin minioadmin123 &>/dev/null; then
        log_error "Failed to set up MinIO alias"
        return 1
    fi

    # List objects in the bucket
    local objects=$(podman exec insights-ros-minio mc ls myminio/insights-ros-data/ --recursive 2>/dev/null)

    # Look for ROS files in the expected path structure (ros/default/source=.../date=.../filename.csv)
    if echo "$objects" | grep -q "ros/.*cost-management.csv\|ros/.*workload-optimization.csv"; then
        log_success "ROS files found in MinIO bucket!"
        echo "$objects" | grep "\.csv"
        return 0
    else
        log_error "ROS files not found in MinIO bucket"
        echo "Bucket contents:"
        echo "$objects"
        return 1
    fi
}

# Function to verify Kafka messages
verify_kafka_messages() {
    log_info "Verifying Kafka messages..."

    # First, verify the topic exists
    log_info "Checking if topic $KAFKA_TOPIC exists..."
    if ! podman exec insights-ros-kafka bin/kafka-topics.sh --bootstrap-server localhost:9092 --list | grep -q "$KAFKA_TOPIC"; then
        log_error "Topic $KAFKA_TOPIC does not exist"
        return 1
    fi

    # Create a temporary file for Kafka consumer output
    local kafka_output=$(mktemp)

    # Start Kafka consumer and capture output (read recent messages)
    log_info "Consuming messages from topic $KAFKA_TOPIC..."
    podman exec insights-ros-kafka bin/kafka-console-consumer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$KAFKA_TOPIC" \
        --from-beginning \
        --timeout-ms 8000 > "$kafka_output" 2>/dev/null || true

    # Check if we received any messages
    if [ -s "$kafka_output" ]; then
        log_success "Kafka messages found in topic $KAFKA_TOPIC:"
        cat "$kafka_output"

        # Verify message contains ROS-related content (since we clean volumes, any message is from this test)
        if grep -q "test-cluster-123\|cost-management.csv\|workload-optimization.csv" "$kafka_output"; then
            log_success "Message contains expected ROS test data!"
        else
            log_warning "Message doesn't contain expected ROS test data"
        fi
    else
        log_warning "No messages found in Kafka topic $KAFKA_TOPIC"
    fi

    # Also check validation topic (optional)
    podman exec insights-ros-kafka bin/kafka-console-consumer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$VALIDATION_TOPIC" \
        --from-beginning \
        --timeout-ms 5000 > "$kafka_output" 2>/dev/null || true

    if [ -s "$kafka_output" ]; then
        log_success "Validation messages found in topic $VALIDATION_TOPIC:"
        cat "$kafka_output"
    fi

    # Cleanup
    rm -f "$kafka_output"
}

# Function to run health checks
run_health_checks() {
    log_info "Running health checks..."

    # Test health endpoint
    local health_response=$(curl -s "http://localhost:$SERVICE_PORT/health")
    log_info "Health check response: $health_response"

    # Test readiness endpoint
    local ready_response=$(curl -s "http://localhost:$SERVICE_PORT/ready")
    log_info "Readiness check response: $ready_response"

    # Test metrics endpoint
    local metrics_response=$(curl -s "http://localhost:$SERVICE_PORT/metrics")
    if echo "$metrics_response" | grep -q "prometheus"; then
        log_success "Metrics endpoint is working!"
    else
        log_warning "Metrics endpoint may not be working properly"
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    stop_ingress_service
    stop_services
    rm -rf "$TEST_DATA_DIR"
}

# Main execution
main() {
    log_info "Starting insights-ros-ingress integration test..."

    # Check required commands
    check_command "podman-compose"
    check_command "curl"
    check_command "nc"
    check_command "make"

    # Set trap for cleanup
    trap cleanup EXIT

    # Change to project root
    cd "$PROJECT_ROOT"

    # Create test data
    create_test_data

    # Start services
    start_services

    # Start the ingress service
    start_ingress_service

    # Run health checks first
    run_health_checks

    # Test upload API
    test_upload_api

    # Wait a moment for processing
    log_info "Waiting for message processing..."
    sleep 5

    # Verify results
    verify_minio_upload
    verify_kafka_messages

    log_success "Integration test completed successfully!"
}

# Run main function if script is executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi