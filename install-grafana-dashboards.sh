#!/bin/bash

# Script to install Grafana dashboards according to victoriametrics-values.yaml.bak
# Dashboards are installed via Grafana API

set -e

# Configuration
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
GRAFANA_USER="${GRAFANA_USER:-admin}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin}"
DATASOURCE_NAME="${DATASOURCE_NAME:-VictoriaMetrics}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to create folder in Grafana
create_folder() {
    local folder_name="$1"
    
    log_info "Creating folder: $folder_name"
    
    # Check if folder exists
    local folder_uid=$(curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        "${GRAFANA_URL}/api/folders" | jq -r ".[] | select(.title==\"${folder_name}\") | .uid")
    
    if [ -n "$folder_uid" ] && [ "$folder_uid" != "null" ]; then
        log_info "Folder '$folder_name' already exists with UID: $folder_uid"
        echo "$folder_uid"
        return
    fi
    
    # Create folder
    local response=$(curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -X POST "${GRAFANA_URL}/api/folders" \
        -d "{\"title\": \"${folder_name}\"}")
    
    folder_uid=$(echo "$response" | jq -r '.uid')
    
    if [ -n "$folder_uid" ] && [ "$folder_uid" != "null" ]; then
        log_info "Created folder '$folder_name' with UID: $folder_uid"
        echo "$folder_uid"
    else
        log_error "Failed to create folder: $response"
        echo ""
    fi
}

# Function to import dashboard from URL
import_dashboard_from_url() {
    local url="$1"
    local folder_uid="$2"
    local datasource="$3"
    local dashboard_name="$4"
    
    log_info "Downloading dashboard: $dashboard_name from $url"
    
    # Download dashboard JSON
    local dashboard_json=$(curl -s "$url")
    
    if [ -z "$dashboard_json" ]; then
        log_error "Failed to download dashboard from $url"
        return 1
    fi
    
    # Prepare import payload - replace datasource and remove id/uid for import
    local import_payload=$(echo "$dashboard_json" | jq --arg ds "$datasource" '
        .id = null |
        .uid = null |
        .templating.list = (.templating.list // [] | map(
            if .type == "datasource" then
                .current.text = $ds |
                .current.value = $ds
            else . end
        )) |
        {
            "dashboard": .,
            "folderUid": "'"$folder_uid"'",
            "overwrite": true,
            "inputs": [
                {
                    "name": "DS_PROMETHEUS",
                    "type": "datasource",
                    "pluginId": "prometheus",
                    "value": $ds
                }
            ]
        }
    ')
    
    # Import dashboard
    local response=$(curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -X POST "${GRAFANA_URL}/api/dashboards/db" \
        -d "$import_payload")
    
    local status=$(echo "$response" | jq -r '.status')
    
    if [ "$status" == "success" ]; then
        log_info "Successfully imported dashboard: $dashboard_name"
    else
        log_warn "Dashboard import response: $response"
    fi
}

# Function to import dashboard from Grafana.net (gnetId)
import_dashboard_from_gnet() {
    local gnet_id="$1"
    local revision="$2"
    local folder_uid="$3"
    local datasource_name="$4"
    local datasource_value="$5"
    local dashboard_name="$6"
    
    log_info "Downloading dashboard: $dashboard_name (gnetId: $gnet_id, revision: $revision)"
    
    # Download from Grafana.net
    local dashboard_json=$(curl -s "https://grafana.com/api/dashboards/${gnet_id}/revisions/${revision}/download")
    
    if [ -z "$dashboard_json" ] || echo "$dashboard_json" | jq -e '.message' > /dev/null 2>&1; then
        log_error "Failed to download dashboard from Grafana.net: $dashboard_json"
        return 1
    fi
    
    # Prepare import payload
    local import_payload=$(jq -n \
        --argjson dashboard "$dashboard_json" \
        --arg folder_uid "$folder_uid" \
        --arg ds_name "$datasource_name" \
        --arg ds_value "$datasource_value" \
        '{
            "dashboard": ($dashboard | .id = null | .uid = null),
            "folderUid": $folder_uid,
            "overwrite": true,
            "inputs": [
                {
                    "name": $ds_name,
                    "type": "datasource",
                    "pluginId": "prometheus",
                    "value": $ds_value
                }
            ]
        }')
    
    # Import dashboard
    local response=$(curl -s -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        -H "Content-Type: application/json" \
        -X POST "${GRAFANA_URL}/api/dashboards/import" \
        -d "$import_payload")
    
    local status=$(echo "$response" | jq -r '.status // .slug')
    
    if [ -n "$status" ] && [ "$status" != "null" ]; then
        log_info "Successfully imported dashboard: $dashboard_name"
    else
        log_warn "Dashboard import response: $response"
    fi
}

# Check if Grafana is accessible
check_grafana_connection() {
    log_info "Checking Grafana connection at $GRAFANA_URL"
    
    local response=$(curl -s -o /dev/null -w "%{http_code}" -u "${GRAFANA_USER}:${GRAFANA_PASSWORD}" \
        "${GRAFANA_URL}/api/health")
    
    if [ "$response" != "200" ]; then
        log_error "Cannot connect to Grafana at $GRAFANA_URL (HTTP $response)"
        log_info "Make sure Grafana is running and credentials are correct"
        log_info "You can set environment variables: GRAFANA_URL, GRAFANA_USER, GRAFANA_PASSWORD"
        exit 1
    fi
    
    log_info "Grafana connection successful"
}

# Main installation function
main() {
    echo "=========================================="
    echo "  Grafana Dashboard Installation Script"
    echo "=========================================="
    echo ""
    
    # Check dependencies
    for cmd in curl jq; do
        if ! command -v $cmd &> /dev/null; then
            log_error "$cmd is required but not installed"
            exit 1
        fi
    done
    
    check_grafana_connection
    
    # ==========================================
    # Strimzi Kafka Dashboards
    # ==========================================
    log_info "Installing Strimzi Kafka dashboards..."
    STRIMZI_FOLDER_UID=$(create_folder "Strimzi Kafka")
    
    if [ -n "$STRIMZI_FOLDER_UID" ]; then
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-kafka.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-kafka"
        
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-operators.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-operators"
        
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-kafka-exporter.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-kafka-exporter"
        
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-kraft.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-kraft"
        
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-cruise-control.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-cruise-control"
        
        import_dashboard_from_url \
            "https://raw.githubusercontent.com/strimzi/strimzi-kafka-operator/main/examples/metrics/grafana-dashboards/strimzi-kafka-bridge.json" \
            "$STRIMZI_FOLDER_UID" \
            "$DATASOURCE_NAME" \
            "strimzi-kafka-bridge"
    fi
    
    # ==========================================
    # Chaos Mesh Dashboards
    # ==========================================
    log_info "Installing Chaos Mesh dashboards..."
    CHAOS_FOLDER_UID=$(create_folder "Chaos Mesh")
    
    if [ -n "$CHAOS_FOLDER_UID" ]; then
        import_dashboard_from_gnet \
            "15918" \
            "1" \
            "$CHAOS_FOLDER_UID" \
            "DS_PROMETHEUS" \
            "$DATASOURCE_NAME" \
            "chaos-mesh-overview"
        
        import_dashboard_from_gnet \
            "15919" \
            "1" \
            "$CHAOS_FOLDER_UID" \
            "DS_PROMETHEUS" \
            "$DATASOURCE_NAME" \
            "chaos-daemon"
    fi
    
    # ==========================================
    # VictoriaLogs Dashboards
    # ==========================================
    log_info "Installing VictoriaLogs dashboards..."
    VLOGS_FOLDER_UID=$(create_folder "VictoriaLogs")
    
    if [ -n "$VLOGS_FOLDER_UID" ]; then
        import_dashboard_from_gnet \
            "24585" \
            "1" \
            "$VLOGS_FOLDER_UID" \
            "DS_VICTORIAMETRICS_LOGS" \
            "victoriametrics-logs" \
            "victorialogs"
    fi
    
    echo ""
    echo "=========================================="
    log_info "Dashboard installation completed!"
    echo "=========================================="
}

# Run main function
main "$@"
