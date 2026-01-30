#!/bin/bash
set -e

# Цвета для вывода
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

# Функция для очистки зависших finalizers в namespace
cleanup_stuck_namespace() {
    local ns=$1
    log_warn "Очистка зависших ресурсов в namespace: $ns"
    kubectl get namespace "$ns" -o json 2>/dev/null | jq '.status.conditions' || true
    kubectl api-resources --verbs=list --namespaced -o name 2>/dev/null | \
        xargs -I {} kubectl get {} -n "$ns" --ignore-not-found 2>/dev/null || true
}

# Проверка наличия kubectl
if ! command -v kubectl &> /dev/null; then
    log_error "kubectl не найден. Установите kubectl."
    exit 1
fi

# Проверка наличия helm
if ! command -v helm &> /dev/null; then
    log_error "helm не найден. Установите helm."
    exit 1
fi

log_info "=== Начало удаления всех компонентов ==="
log_warn "ВАЖНО: Порядок критичен! Strimzi CRD имеют finalizers, требующие работающего оператора."

# 1. Chaos experiments и Chaos Mesh
log_info "Шаг 1: Удаление Chaos experiments и Chaos Mesh..."
kubectl delete -f chaos-experiments/ --ignore-not-found --wait=false 2>/dev/null || true

for r in iochaos timechaos dnschaos networkchaos podchaos stresschaos httpchaos jvmchaos; do
    kubectl delete "$r" --all -n kafka-cluster --ignore-not-found --wait=false 2>/dev/null || true
done

kubectl delete -f chaos-mesh-servicemonitor.yaml --ignore-not-found 2>/dev/null || true
helm uninstall chaos-mesh -n chaos-mesh --wait --timeout=2m 2>/dev/null || true
kubectl delete -f chaos-mesh-rbac.yaml --ignore-not-found 2>/dev/null || true
log_info "Chaos Mesh удалён."

# 2. Observability и приложения (параллельно)
log_info "Шаг 2: Удаление Observability и приложений..."
helm uninstall victoria-logs-cluster -n victoria-logs-cluster --wait --timeout=2m 2>/dev/null &
helm uninstall victoria-logs-collector -n victoria-logs-collector --wait --timeout=2m 2>/dev/null &
helm uninstall vmks -n vmks --wait --timeout=2m 2>/dev/null &
helm uninstall kafka-producer -n kafka-producer --wait --timeout=2m 2>/dev/null &
helm uninstall kafka-consumer -n kafka-consumer --wait --timeout=2m 2>/dev/null &
helm uninstall kafka-ui -n kafka-ui --wait --timeout=2m 2>/dev/null &
wait

kubectl delete -f schema-registry.yaml --ignore-not-found --wait=false 2>/dev/null || true
kubectl delete -f kafka-servicemonitor.yaml -f strimzi/kafka-pdb.yaml --ignore-not-found 2>/dev/null || true
log_info "Observability и приложения удалены."

# 3. Kafka ресурсы (ПОКА ОПЕРАТОР РАБОТАЕТ!)
log_info "Шаг 3: Удаление Kafka ресурсов (topics, users)..."
log_warn "Оператор Strimzi должен работать для корректного удаления!"

kubectl delete kafkatopic,kafkauser --all -n kafka-cluster --ignore-not-found --wait=false 2>/dev/null || true
kubectl wait --for=delete kafkatopic,kafkauser --all -n kafka-cluster --timeout=120s 2>/dev/null || true

for r in kafkatopic kafkauser; do
    kubectl get "$r" -n kafka-cluster -o name 2>/dev/null | \
        xargs -I {} kubectl patch {} -n kafka-cluster -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
done
log_info "Kafka ресурсы удалены."

# 4. Kafka cluster
log_info "Шаг 4: Удаление Kafka cluster..."
kubectl delete kafka,kafkanodepool --all -n kafka-cluster --ignore-not-found --wait=false 2>/dev/null || true
kubectl wait --for=delete kafka --all -n kafka-cluster --timeout=180s 2>/dev/null || true
kubectl delete -f strimzi/kafka-metrics-config.yaml --ignore-not-found 2>/dev/null || true
log_info "Kafka cluster удалён."

# 5. Strimzi оператор (ПОСЛЕ удаления Kafka ресурсов!)
log_info "Шаг 5: Удаление Strimzi оператора..."
helm uninstall strimzi-cluster-operator -n strimzi --wait --timeout=2m 2>/dev/null || true
log_info "Strimzi оператор удалён."

# 6. Prometheus CRDs (после удаления всех мониторинг ресурсов)
log_info "Шаг 6: Удаление Prometheus CRDs..."
helm uninstall prometheus-operator-crds -n prometheus-crds --wait --timeout=2m 2>/dev/null || true
log_info "Prometheus CRDs удалены."

# 7. Namespaces
log_info "Шаг 7: Удаление namespaces..."
NS="chaos-mesh victoria-logs-cluster victoria-logs-collector vmks kafka-producer kafka-consumer kafka-ui schema-registry kafka-cluster strimzi prometheus-crds"

for ns in $NS; do
    kubectl delete ns "$ns" --ignore-not-found 2>/dev/null &
done
wait

# Проверка на зависшие namespaces
log_info "Проверка на зависшие namespaces..."
sleep 5

STUCK_NS=""
for ns in $NS; do
    if kubectl get ns "$ns" &>/dev/null; then
        STUCK_NS="$STUCK_NS $ns"
    fi
done

if [ -n "$STUCK_NS" ]; then
    log_warn "Обнаружены зависшие namespaces:$STUCK_NS"
    log_info "Для диагностики зависших namespace используйте:"
    echo "  kubectl get namespace <ns> -o json | jq '.status.conditions'"
    echo "  kubectl api-resources --verbs=list --namespaced -o name | xargs -I {} kubectl get {} -n <ns> --ignore-not-found"
    echo "  kubectl patch <type> <name> -n <ns> -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
else
    log_info "Все namespaces успешно удалены."
fi

log_info "=== Удаление завершено ==="
