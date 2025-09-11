#!/bin/bash

# Script to verify MinIO uploads and ROS data extraction
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
MINIO_CONTAINER="insights-ros-minio"
BUCKET_NAME="insights-ros-data"
MINIO_ALIAS="myminio"

log_info() { echo -e "${BLUE}[$(date '+%H:%M:%S')] $*${NC}"; }
log_success() { echo -e "${GREEN}[$(date '+%H:%M:%S')] $*${NC}"; }
log_warning() { echo -e "${YELLOW}[$(date '+%H:%M:%S')] $*${NC}"; }
log_error() { echo -e "${RED}[$(date '+%H:%M:%S')] $*${NC}"; }

# Function to check if MinIO container is running
check_minio_container() {
    if ! podman ps --filter "name=$MINIO_CONTAINER" --format "table {{.Names}}" | grep -q "$MINIO_CONTAINER"; then
        log_error "MinIO container '$MINIO_CONTAINER' is not running"
        log_info "Start it with: make dev-env-up"
        exit 1
    fi
    log_success "MinIO container is running"
}

# Function to check MinIO connectivity
check_minio_connectivity() {
    log_info "Checking MinIO connectivity..."

    if podman exec "$MINIO_CONTAINER" mc admin info "$MINIO_ALIAS" >/dev/null 2>&1; then
        log_success "MinIO is accessible"
    else
        log_error "Cannot connect to MinIO"
        return 1
    fi
}

# Function to list buckets
list_buckets() {
    log_info "Listing MinIO buckets..."
    podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS"
}

# Function to check if bucket exists
check_bucket() {
    log_info "Checking if bucket '$BUCKET_NAME' exists..."

    if podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS/$BUCKET_NAME" >/dev/null 2>&1; then
        log_success "Bucket '$BUCKET_NAME' exists"
        return 0
    else
        log_warning "Bucket '$BUCKET_NAME' does not exist"
        return 1
    fi
}

# Function to list bucket contents
list_bucket_contents() {
    local prefix=${1:-""}
    log_info "Listing contents of bucket '$BUCKET_NAME'${prefix:+ with prefix '$prefix'}..."

    if check_bucket; then
        local objects=$(podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS/$BUCKET_NAME/$prefix" --recursive)
        if [ -n "$objects" ]; then
            echo "$objects"

            # Count different file types
            local csv_count=$(echo "$objects" | grep -c "\.csv$" || true)
            local json_count=$(echo "$objects" | grep -c "\.json$" || true)
            local total_count=$(echo "$objects" | wc -l)

            log_info "Summary: $total_count total files, $csv_count CSV files, $json_count JSON files"
        else
            log_warning "No objects found in bucket"
        fi
    fi
}

# Function to analyze ROS files in bucket
analyze_ros_files() {
    log_info "Analyzing ROS files in bucket..."

    if ! check_bucket; then
        return 1
    fi

    # Look for typical ROS file patterns
    local ros_patterns=("cost-management" "workload-optimization" "node-optimization" "resource-optimization")

    for pattern in "${ros_patterns[@]}"; do
        local files=$(podman exec "$MINIO_CONTAINER" mc find "$MINIO_ALIAS/$BUCKET_NAME" --name "*$pattern*" 2>/dev/null || true)
        if [ -n "$files" ]; then
            log_success "Found files matching '$pattern':"
            echo "$files" | sed 's/^/  /'
        fi
    done

    # Check for files uploaded in the last hour
    log_info "Files uploaded in the last hour:"
    local recent_files=$(podman exec "$MINIO_CONTAINER" mc find "$MINIO_ALIAS/$BUCKET_NAME" --newer-than 1h 2>/dev/null || true)
    if [ -n "$recent_files" ]; then
        echo "$recent_files" | sed 's/^/  /'
    else
        log_warning "No files uploaded in the last hour"
    fi
}

# Function to download and examine a file
examine_file() {
    local file_path=$1
    local temp_file=$(mktemp)

    log_info "Examining file: $file_path"

    if podman exec "$MINIO_CONTAINER" mc cp "$MINIO_ALIAS/$BUCKET_NAME/$file_path" /tmp/examine_file.tmp >/dev/null 2>&1; then
        podman exec "$MINIO_CONTAINER" cat /tmp/examine_file.tmp > "$temp_file"

        # Show file info
        local size=$(wc -c < "$temp_file")
        local lines=$(wc -l < "$temp_file")
        log_info "File size: $size bytes, Lines: $lines"

        # Show first few lines
        log_info "First 10 lines:"
        head -10 "$temp_file" | sed 's/^/  /'

        # If it's a CSV, show column headers
        if [[ "$file_path" == *.csv ]]; then
            log_info "CSV columns:"
            head -1 "$temp_file" | tr ',' '\n' | nl | sed 's/^/  /'
        fi

        # Check for specific ROS content
        if grep -q "cpu_request\|memory_request\|cost\|optimization\|recommendation" "$temp_file"; then
            log_success "File contains ROS-related data"
        fi

        podman exec "$MINIO_CONTAINER" rm -f /tmp/examine_file.tmp
    else
        log_error "Failed to download file: $file_path"
    fi

    rm -f "$temp_file"
}

# Function to search for files by request ID
search_by_request_id() {
    local request_id=$1
    log_info "Searching for files with request ID: $request_id"

    local files=$(podman exec "$MINIO_CONTAINER" mc find "$MINIO_ALIAS/$BUCKET_NAME" --name "*$request_id*" 2>/dev/null || true)
    if [ -n "$files" ]; then
        log_success "Found files for request ID '$request_id':"
        echo "$files" | sed 's/^/  /'

        # Examine the first file found
        local first_file=$(echo "$files" | head -1 | sed "s|$MINIO_ALIAS/$BUCKET_NAME/||")
        if [ -n "$first_file" ]; then
            examine_file "$first_file"
        fi
    else
        log_warning "No files found for request ID: $request_id"
    fi
}

# Function to verify bucket structure
verify_bucket_structure() {
    log_info "Verifying bucket structure..."

    if ! check_bucket; then
        return 1
    fi

    # Check for expected directory structure
    local prefixes=("ros/" "hccm/" "cost-management/" "workload-optimization/")

    for prefix in "${prefixes[@]}"; do
        local objects=$(podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS/$BUCKET_NAME/$prefix" 2>/dev/null || true)
        if [ -n "$objects" ]; then
            log_success "Found objects in prefix: $prefix"
        fi
    done

    # Check for partitioning patterns (date-based, source-based, etc.)
    log_info "Checking for partitioning patterns..."
    local all_objects=$(podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS/$BUCKET_NAME" --recursive 2>/dev/null || true)

    if echo "$all_objects" | grep -q "source="; then
        log_success "Found source-based partitioning"
    fi

    if echo "$all_objects" | grep -q "date="; then
        log_success "Found date-based partitioning"
    fi
}

# Function to test upload (for debugging)
test_upload() {
    local test_file=$(mktemp)
    echo "test,data,$(date)" > "$test_file"

    local test_path="test/$(date +%s)/test.csv"

    log_info "Testing upload to path: $test_path"

    if podman exec "$MINIO_CONTAINER" mc cp /dev/stdin "$MINIO_ALIAS/$BUCKET_NAME/$test_path" < "$test_file"; then
        log_success "Test upload successful"

        # Verify the upload
        if podman exec "$MINIO_CONTAINER" mc ls "$MINIO_ALIAS/$BUCKET_NAME/$test_path" >/dev/null; then
            log_success "Test file verified in bucket"

            # Clean up test file
            podman exec "$MINIO_CONTAINER" mc rm "$MINIO_ALIAS/$BUCKET_NAME/$test_path"
            log_info "Test file cleaned up"
        fi
    else
        log_error "Test upload failed"
    fi

    rm -f "$test_file"
}

# Function to show storage usage
show_storage_usage() {
    log_info "MinIO storage usage:"
    podman exec "$MINIO_CONTAINER" mc admin info "$MINIO_ALIAS"
}

# Main function
main() {
    local action=${1:-"verify"}

    case "$action" in
        "verify")
            log_info "Verifying MinIO setup and ROS data..."
            check_minio_container
            check_minio_connectivity
            list_buckets
            check_bucket
            list_bucket_contents
            analyze_ros_files
            verify_bucket_structure
            ;;
        "list")
            check_minio_container
            list_bucket_contents "${2:-}"
            ;;
        "analyze")
            check_minio_container
            analyze_ros_files
            ;;
        "examine")
            if [ -z "${2:-}" ]; then
                log_error "Usage: $0 examine <file-path>"
                exit 1
            fi
            check_minio_container
            examine_file "$2"
            ;;
        "search")
            if [ -z "${2:-}" ]; then
                log_error "Usage: $0 search <request-id>"
                exit 1
            fi
            check_minio_container
            search_by_request_id "$2"
            ;;
        "test")
            check_minio_container
            test_upload
            ;;
        "usage")
            check_minio_container
            show_storage_usage
            ;;
        *)
            echo "Usage: $0 [verify|list|analyze|examine|search|test|usage]"
            echo ""
            echo "Commands:"
            echo "  verify            - Verify MinIO setup and analyze ROS data (default)"
            echo "  list [prefix]     - List bucket contents, optionally with prefix"
            echo "  analyze           - Analyze ROS files in bucket"
            echo "  examine <file>    - Download and examine a specific file"
            echo "  search <req-id>   - Search for files by request ID"
            echo "  test              - Test upload functionality"
            echo "  usage             - Show storage usage information"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"