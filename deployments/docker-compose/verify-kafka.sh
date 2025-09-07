#!/bin/bash

# Script to verify Kafka messages for insights-ros-ingress
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
KAFKA_CONTAINER="insights-ros-kafka"
ROS_TOPIC="hccm.ros.events"
VALIDATION_TOPIC="platform.upload.validation"
TIMEOUT=30

log_info() { echo -e "${BLUE}[$(date '+%H:%M:%S')] $*${NC}"; }
log_success() { echo -e "${GREEN}[$(date '+%H:%M:%S')] $*${NC}"; }
log_warning() { echo -e "${YELLOW}[$(date '+%H:%M:%S')] $*${NC}"; }
log_error() { echo -e "${RED}[$(date '+%H:%M:%S')] $*${NC}"; }

# Function to check if Kafka container is running
check_kafka_container() {
    if ! podman ps --filter "name=$KAFKA_CONTAINER" --format "table {{.Names}}" | grep -q "$KAFKA_CONTAINER"; then
        log_error "Kafka container '$KAFKA_CONTAINER' is not running"
        log_info "Start it with: make dev-env-up"
        exit 1
    fi
    log_success "Kafka container is running"
}

# Function to list Kafka topics
list_topics() {
    log_info "Listing Kafka topics..."
    podman exec "$KAFKA_CONTAINER" bin/kafka-topics.sh \
        --bootstrap-server localhost:9092 \
        --list
}

# Function to check specific topic
check_topic() {
    local topic=$1
    log_info "Checking if topic '$topic' exists..."

    if podman exec "$KAFKA_CONTAINER" bin/kafka-topics.sh \
        --bootstrap-server localhost:9092 \
        --list | grep -q "^$topic$"; then
        log_success "Topic '$topic' exists"
        return 0
    else
        log_warning "Topic '$topic' does not exist"
        return 1
    fi
}

# Function to consume messages from a topic
consume_messages() {
    local topic=$1
    local timeout=${2:-$TIMEOUT}
    local output_file=$(mktemp)

    log_info "Consuming messages from topic '$topic' (timeout: ${timeout}s)..."

    # Consume messages with timeout
    timeout "${timeout}s" podman exec "$KAFKA_CONTAINER" bin/kafka-console-consumer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$topic" \
        --from-beginning \
        --timeout-ms $((timeout * 1000)) > "$output_file" 2>/dev/null || true

    if [ -s "$output_file" ]; then
        local msg_count=$(wc -l < "$output_file")
        log_success "Found $msg_count message(s) in topic '$topic':"
        echo "----------------------------------------"
        cat "$output_file" | head -20  # Show first 20 messages
        if [ "$msg_count" -gt 20 ]; then
            echo "... and $((msg_count - 20)) more messages"
        fi
        echo "----------------------------------------"

        # Analyze ROS messages
        if [ "$topic" = "$ROS_TOPIC" ]; then
            analyze_ros_messages "$output_file"
        fi

        # Analyze validation messages
        if [ "$topic" = "$VALIDATION_TOPIC" ]; then
            analyze_validation_messages "$output_file"
        fi
    else
        log_warning "No messages found in topic '$topic'"
    fi

    rm -f "$output_file"
}

# Function to analyze ROS messages
analyze_ros_messages() {
    local file=$1
    log_info "Analyzing ROS messages..."

    # Check for required fields in ROS messages
    local request_ids=$(grep -o '"request_id":"[^"]*"' "$file" | cut -d'"' -f4 | sort | uniq)
    local org_ids=$(grep -o '"org_id":"[^"]*"' "$file" | cut -d'"' -f4 | sort | uniq)
    local file_counts=$(grep -o '"files":\[[^\]]*\]' "$file" | wc -l)

    if [ -n "$request_ids" ]; then
        log_success "Request IDs found: $(echo "$request_ids" | tr '\n' ' ')"
    fi

    if [ -n "$org_ids" ]; then
        log_success "Org IDs found: $(echo "$org_ids" | tr '\n' ' ')"
    fi

    if [ "$file_counts" -gt 0 ]; then
        log_success "Messages with file lists: $file_counts"
    fi

    # Check for ROS-specific files
    if grep -q "cost-management.csv\|workload-optimization.csv\|node-optimization.csv" "$file"; then
        log_success "ROS CSV files detected in messages"
    else
        log_warning "No ROS CSV files found in messages"
    fi
}

# Function to analyze validation messages
analyze_validation_messages() {
    local file=$1
    log_info "Analyzing validation messages..."

    local validation_statuses=$(grep -o '"validation":"[^"]*"' "$file" | cut -d'"' -f4 | sort | uniq)

    if [ -n "$validation_statuses" ]; then
        log_success "Validation statuses found: $(echo "$validation_statuses" | tr '\n' ' ')"
    fi
}

# Function to send a test message (for debugging)
send_test_message() {
    local topic=$1
    local message=$2

    log_info "Sending test message to topic '$topic'..."
    echo "$message" | podman exec -i "$KAFKA_CONTAINER" bin/kafka-console-producer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$topic"
    log_success "Test message sent"
}

# Function to monitor topics in real-time
monitor_topics() {
    log_info "Monitoring topics in real-time (press Ctrl+C to stop)..."

    # Start consumers for both topics in background
    podman exec "$KAFKA_CONTAINER" bin/kafka-console-consumer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$ROS_TOPIC" \
        --property print.timestamp=true \
        --property print.key=true &
    local ros_pid=$!

    podman exec "$KAFKA_CONTAINER" bin/kafka-console-consumer.sh \
        --bootstrap-server localhost:9092 \
        --topic "$VALIDATION_TOPIC" \
        --property print.timestamp=true \
        --property print.key=true &
    local validation_pid=$!

    # Wait for Ctrl+C
    trap "kill $ros_pid $validation_pid 2>/dev/null || true; exit 0" INT
    wait
}

# Function to get topic information
get_topic_info() {
    local topic=$1
    log_info "Getting information for topic '$topic'..."

    podman exec "$KAFKA_CONTAINER" bin/kafka-topics.sh \
        --bootstrap-server localhost:9092 \
        --describe \
        --topic "$topic"
}

# Main function
main() {
    local action=${1:-"verify"}

    case "$action" in
        "verify")
            log_info "Verifying Kafka setup and messages..."
            check_kafka_container
            list_topics
            check_topic "$ROS_TOPIC"
            check_topic "$VALIDATION_TOPIC"
            consume_messages "$ROS_TOPIC"
            consume_messages "$VALIDATION_TOPIC"
            ;;
        "monitor")
            check_kafka_container
            monitor_topics
            ;;
        "info")
            check_kafka_container
            get_topic_info "$ROS_TOPIC"
            get_topic_info "$VALIDATION_TOPIC"
            ;;
        "ros")
            check_kafka_container
            consume_messages "$ROS_TOPIC" 60
            ;;
        "validation")
            check_kafka_container
            consume_messages "$VALIDATION_TOPIC" 60
            ;;
        "test")
            check_kafka_container
            send_test_message "$ROS_TOPIC" '{"test":"message","timestamp":"'$(date -Iseconds)'"}'
            ;;
        *)
            echo "Usage: $0 [verify|monitor|info|ros|validation|test]"
            echo ""
            echo "Commands:"
            echo "  verify     - Verify Kafka setup and consume existing messages (default)"
            echo "  monitor    - Monitor topics in real-time"
            echo "  info       - Show topic information"
            echo "  ros        - Consume only ROS topic messages"
            echo "  validation - Consume only validation topic messages"
            echo "  test       - Send a test message to ROS topic"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"