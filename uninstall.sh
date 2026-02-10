#!/bin/bash
set -euo pipefail

echo "=== Удаление компонентов Strimzi Kafka Chaos Testing ==="

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Функция для проверки существования ресурса
check_resource() {
    local resource=$1
    local namespace=${2:-}
    if [ -n "$namespace" ]; then
        kubectl get "$resource" -n "$namespace" &>/dev/null && return 0 || return 1
    else
        kubectl get "$resource" &>/dev/null && return 0 || return 1
    fi
}

echo -e "${YELLOW}1. Удаление Chaos Mesh экспериментов...${NC}"
kubectl delete -f chaos-experiments/ --ignore-not-found=true 2>/dev/null || true
echo -e "${GREEN}✓ Эксперименты удалены${NC}"

echo -e "${YELLOW}2. Удаление Producer/Consumer...${NC}"
helm uninstall kafka-producer -n kafka-producer 2>/dev/null || true
helm uninstall kafka-consumer -n kafka-consumer 2>/dev/null || true
kubectl delete namespace kafka-producer kafka-consumer --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ Producer/Consumer удалены${NC}"

echo -e "${YELLOW}3. Удаление Kafka UI...${NC}"
helm uninstall kafka-ui -n kafka-ui 2>/dev/null || true
kubectl delete namespace kafka-ui --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ Kafka UI удалён${NC}"

echo -e "${YELLOW}4. Удаление Schema Registry...${NC}"
kubectl delete -f schema-registry.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete namespace schema-registry --ignore-not-found=true --timeout=60s || true
kubectl delete kafkauser schema-registry -n kafka-cluster --ignore-not-found=true 2>/dev/null || true
kubectl delete kafkatopic schemas-topic -n kafka-cluster --ignore-not-found=true 2>/dev/null || true
echo -e "${GREEN}✓ Schema Registry удалён${NC}"

echo -e "${YELLOW}5. Удаление VictoriaLogs Collector...${NC}"
helm uninstall victoria-logs-collector -n victoria-logs-collector 2>/dev/null || true
kubectl delete namespace victoria-logs-collector --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ VictoriaLogs Collector удалён${NC}"

echo -e "${YELLOW}6. Удаление VictoriaLogs Cluster...${NC}"
helm uninstall victoria-logs-cluster -n victoria-logs-cluster 2>/dev/null || true
kubectl delete namespace victoria-logs-cluster --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ VictoriaLogs Cluster удалён${NC}"

echo -e "${YELLOW}7. Удаление Kafka кластера и Strimzi...${NC}"
echo "  Удаление Kafka CR..."
kubectl delete kafka kafka-cluster -n kafka-cluster --ignore-not-found=true 2>/dev/null || true
kubectl delete kafkatopic test-topic schemas-topic -n kafka-cluster --ignore-not-found=true 2>/dev/null || true
kubectl delete kafkauser myuser schema-registry kafka-ui-user -n kafka-cluster --ignore-not-found=true 2>/dev/null || true

echo "  Ожидание удаления Kafka (до 5 минут)..."
kubectl wait kafka/kafka-cluster -n kafka-cluster --for=delete --timeout=300s 2>/dev/null || true

echo "  Удаление метрик и других ресурсов..."
kubectl delete -f strimzi/kafka-exporter-servicemonitor.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kafka-producer-metrics.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kafka-consumer-metrics.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/cluster-operator-metrics.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/entity-operator-metrics.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kafka-resources-metrics.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kube-state-metrics-ksm.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kube-state-metrics-configmap.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f strimzi/kafka-pdb.yaml --ignore-not-found=true 2>/dev/null || true

echo "  Удаление Strimzi Operator..."
helm uninstall strimzi-cluster-operator -n strimzi 2>/dev/null || true
kubectl delete namespace strimzi --ignore-not-found=true --timeout=60s || true
kubectl delete namespace kafka-cluster --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ Kafka и Strimzi удалены${NC}"

echo -e "${YELLOW}8. Удаление VictoriaMetrics K8s Stack...${NC}"
helm uninstall vmks -n vmks 2>/dev/null || true
kubectl delete namespace vmks --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ VictoriaMetrics K8s Stack удалён${NC}"

echo -e "${YELLOW}9. Удаление Chaos Mesh...${NC}"
kubectl delete -f chaos-mesh/chaos-mesh-vmservicescrape.yaml --ignore-not-found=true 2>/dev/null || true
kubectl delete -f chaos-mesh/chaos-mesh-rbac.yaml --ignore-not-found=true 2>/dev/null || true
helm uninstall chaos-mesh -n chaos-mesh 2>/dev/null || true
kubectl delete namespace chaos-mesh --ignore-not-found=true --timeout=60s || true
echo -e "${GREEN}✓ Chaos Mesh удалён${NC}"

echo ""
echo -e "${GREEN}=== Все компоненты удалены ===${NC}"
echo ""
echo "Примечание: PVC для Kafka и VictoriaLogs не удаляются автоматически."
echo "Удалите их вручную при необходимости:"
echo "  kubectl get pvc -A | grep -E 'kafka-cluster|victoria-logs'"
echo "  kubectl delete pvc <pvc-name> -n <namespace>"
