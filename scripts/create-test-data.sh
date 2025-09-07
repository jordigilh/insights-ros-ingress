#!/bin/bash

# Script to create test data for insights-ros-ingress testing
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DATA_DIR="$SCRIPT_DIR/test-data"

# Create test data directory
mkdir -p "$TEST_DATA_DIR"

echo "Creating test data in $TEST_DATA_DIR..."

# Create a temporary directory for test files
TEMP_DIR=$(mktemp -d)

# Create manifest.json with ROS files
cat > "$TEMP_DIR/manifest.json" << 'EOF'
{
    "version": "1.0",
    "cluster_id": "test-cluster-123",
    "cluster_alias": "test-cluster",
    "source_metadata": {
        "any": {
            "cluster_version": "4.14.0",
            "cluster_id": "test-cluster-123",
            "platform_version": "4.14.0-0.okd-2024-01-01-123456"
        }
    },
    "resource_optimization_files": [
        "cost-management.csv",
        "workload-optimization.csv",
        "node-optimization.csv"
    ],
    "files": [
        "manifest.json",
        "cost-management.csv",
        "workload-optimization.csv",
        "node-optimization.csv",
        "other-data.json",
        "cluster-info.yaml"
    ]
}
EOF

# Create sample ROS CSV files with realistic data
cat > "$TEMP_DIR/cost-management.csv" << 'EOF'
date,namespace,pod,cpu_request,cpu_usage,memory_request,memory_usage,cost_per_hour,recommendations
2024-01-01T00:00:00Z,default,web-app-1,100m,50m,128Mi,64Mi,0.025,reduce_cpu
2024-01-01T00:00:00Z,default,web-app-2,100m,45m,128Mi,72Mi,0.025,reduce_cpu
2024-01-01T00:00:00Z,default,web-app-3,100m,55m,128Mi,68Mi,0.025,reduce_cpu
2024-01-01T00:00:00Z,kube-system,coredns-1,100m,80m,70Mi,65Mi,0.015,optimal
2024-01-01T00:00:00Z,kube-system,coredns-2,100m,75m,70Mi,62Mi,0.015,optimal
2024-01-01T00:00:00Z,monitoring,prometheus,500m,400m,1Gi,850Mi,0.120,reduce_memory
2024-01-01T00:00:00Z,monitoring,grafana,100m,60m,256Mi,180Mi,0.035,reduce_memory
2024-01-01T00:00:00Z,logging,elasticsearch-1,200m,180m,512Mi,480Mi,0.065,optimal
2024-01-01T00:00:00Z,logging,elasticsearch-2,200m,175m,512Mi,475Mi,0.065,optimal
2024-01-01T00:00:00Z,logging,kibana,100m,70m,256Mi,200Mi,0.035,reduce_cpu
EOF

cat > "$TEMP_DIR/workload-optimization.csv" << 'EOF'
namespace,workload,type,current_replicas,recommended_replicas,cpu_request,recommended_cpu,memory_request,recommended_memory,confidence,savings_potential
default,web-app,Deployment,3,2,100m,75m,128Mi,96Mi,85,25%
kube-system,coredns,Deployment,2,2,100m,100m,70Mi,70Mi,95,0%
monitoring,prometheus,StatefulSet,1,1,500m,400m,1Gi,768Mi,90,20%
monitoring,grafana,Deployment,1,1,100m,75m,256Mi,192Mi,80,15%
logging,elasticsearch,StatefulSet,2,2,200m,200m,512Mi,512Mi,95,0%
logging,kibana,Deployment,1,1,100m,75m,256Mi,200Mi,75,12%
ingress-nginx,controller,DaemonSet,3,3,100m,100m,90Mi,90Mi,95,0%
cert-manager,webhook,Deployment,1,1,10m,10m,32Mi,32Mi,90,0%
EOF

cat > "$TEMP_DIR/node-optimization.csv" << 'EOF'
node_name,cpu_capacity,cpu_requests,cpu_usage,memory_capacity,memory_requests,memory_usage,pod_count,max_pods,utilization_score,recommendations
worker-1,4000m,2500m,2000m,8Gi,4Gi,3.2Gi,15,110,75,add_workloads
worker-2,4000m,3200m,2800m,8Gi,5.5Gi,4.8Gi,22,110,85,optimal
worker-3,4000m,1800m,1200m,8Gi,2.5Gi,2Gi,8,110,45,consolidate_workloads
master-1,2000m,800m,600m,4Gi,1.5Gi,1.2Gi,12,110,35,control_plane_only
master-2,2000m,750m,580m,4Gi,1.4Gi,1.1Gi,11,110,33,control_plane_only
master-3,2000m,820m,620m,4Gi,1.6Gi,1.3Gi,13,110,37,control_plane_only
EOF

# Create additional files to make the payload more realistic
cat > "$TEMP_DIR/other-data.json" << 'EOF'
{
    "collection_timestamp": "2024-01-01T00:00:00Z",
    "cluster_metadata": {
        "openshift_version": "4.14.0",
        "kubernetes_version": "v1.27.0",
        "node_count": 6,
        "pod_count": 82
    },
    "non_ros_data": true
}
EOF

cat > "$TEMP_DIR/cluster-info.yaml" << 'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-info
  namespace: kube-public
data:
  cluster-id: test-cluster-123
  cluster-name: test-cluster
  platform: openshift
EOF

# Create tar.gz archive
echo "Creating test payload archive..."
(cd "$TEMP_DIR" && tar -czf "$TEST_DATA_DIR/test-payload.tar.gz" .)

# Create a simpler payload for quick tests
echo "Creating simple test payload..."
mkdir -p "$TEMP_DIR/simple"
echo '{"files":["simple.csv"],"resource_optimization_files":["simple.csv"]}' > "$TEMP_DIR/simple/manifest.json"
echo "date,pod,cpu,memory" > "$TEMP_DIR/simple/simple.csv"
echo "2024-01-01,test-pod,100m,128Mi" >> "$TEMP_DIR/simple/simple.csv"
(cd "$TEMP_DIR/simple" && tar -czf "$TEST_DATA_DIR/simple-payload.tar.gz" .)

# Create an invalid payload (no ROS files)
echo "Creating invalid test payload..."
mkdir -p "$TEMP_DIR/invalid"
echo '{"files":["data.txt"]}' > "$TEMP_DIR/invalid/manifest.json"
echo "This is not a ROS file" > "$TEMP_DIR/invalid/data.txt"
(cd "$TEMP_DIR/invalid" && tar -czf "$TEST_DATA_DIR/invalid-payload.tar.gz" .)

# Cleanup temp directory
rm -rf "$TEMP_DIR"

echo "Test data created successfully:"
echo "  - Full payload: $TEST_DATA_DIR/test-payload.tar.gz"
echo "  - Simple payload: $TEST_DATA_DIR/simple-payload.tar.gz"
echo "  - Invalid payload: $TEST_DATA_DIR/invalid-payload.tar.gz"

# List the contents for verification
echo ""
echo "Archive contents:"
echo "=== test-payload.tar.gz ==="
tar -tzf "$TEST_DATA_DIR/test-payload.tar.gz"
echo ""
echo "=== simple-payload.tar.gz ==="
tar -tzf "$TEST_DATA_DIR/simple-payload.tar.gz"