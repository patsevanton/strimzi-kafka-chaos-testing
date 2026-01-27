# Дополнительные компоненты (пока не используются)

В этом файле собраны компоненты, которые можно подключать **опционально** (Chaos Engineering и Observability). Сейчас они вынесены из основного `README.md`, потому что на текущем этапе не нужны.

## Chaos Mesh

**Chaos Mesh** — платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

### Установка Chaos Mesh

Для доступа к Dashboard через `ingress-nginx` используйте файл `chaos-mesh-values.yaml` из репозитория.

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh \
  --create-namespace \
  -f chaos-mesh-values.yaml \
  --wait
```

Проверка установки:

```bash
kubectl get pods -n chaos-mesh
```

## Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из компонентов кластера Kafka и приложений.

### VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

#### Установка: Cluster

Для установки используйте `victorialogs-cluster-values.yaml` из репозитория.

```bash
helm upgrade --install victoria-logs-cluster \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-cluster \
  --namespace victoria-logs-cluster \
  --create-namespace \
  --wait \
  --version 0.0.25 \
  --timeout 15m \
  -f victorialogs-cluster-values.yaml
```

### victoria-logs-collector

`victoria-logs-collector` — Helm-чарт от VictoriaMetrics, разворачивающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилище.

#### Установка

Для установки используйте `victorialogs-collector-values.yaml` из репозитория.

```bash
helm upgrade --install victoria-logs-collector \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-collector \
  --namespace victoria-logs-collector \
  --create-namespace \
  --wait \
  --version 0.2.5 \
  --timeout 15m \
  -f victorialogs-collector-values.yaml
```

### VictoriaMetrics (VM K8s Stack)

`victoria-metrics-k8s-stack` — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

#### Установка

Для установки используйте `vmks-values.yaml` из репозитория.

```bash
helm upgrade --install vmks \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-metrics-k8s-stack \
  --namespace vmks \
  --create-namespace \
  --wait \
  --version 0.66.1 \
  --timeout 15m \
  -f vmks-values.yaml
```

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```

### Генерация нагрузки (без сборки образа)

На момент написания README образ приложения в `ghcr.io/patsevanton/strimzi-kafka-chaos-testing` использует `segmentio/kafka-go` и может требовать обновления/пересборки для полной совместимости с Kafka `4.x`. Чтобы **гарантированно** получить нагрузку без сборки образов, используйте Kafka CLI из образа Strimzi.

Если вы не хотите собирать/публиковать Docker-образ приложения, можно генерировать нагрузку штатными утилитами Kafka из образа Strimzi.

```bash
kubectl create namespace kafka-app --dry-run=client -o yaml | kubectl apply -f -

# Под-утилита с Kafka CLI
kubectl run kafka-client -n kafka-app \
  --image=quay.io/strimzi/kafka:0.50.0-kafka-4.1.1 \
  --restart=Never --command -- sleep 3600
kubectl wait pod/kafka-client -n kafka-app --for=condition=Ready --timeout=120s

# Produce нагрузка (пример)
PASS=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
kubectl exec -n kafka-app kafka-client -- bash -lc "\
cat > /tmp/client.properties <<'EOF'
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-512
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username=\"myuser\" password=\"$PASS\";
EOF
/opt/kafka/bin/kafka-producer-perf-test.sh \
  --topic test-topic \
  --num-records 100000 \
  --record-size 256 \
  --throughput -1 \
  --producer-props bootstrap.servers=kafka-cluster-kafka-bootstrap.kafka-cluster:9092 \
  --producer.config /tmp/client.properties"
```

## Удаление (только для этих компонентов)

```bash
# Удаление Chaos Mesh
helm uninstall chaos-mesh -n chaos-mesh

# Удаление VictoriaLogs
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
helm uninstall victoria-logs-collector -n victoria-logs-collector

# Удаление VictoriaMetrics K8s Stack
helm uninstall vmks -n vmks

# Удаление namespace (если не используете их больше нигде)
kubectl delete namespace chaos-mesh
kubectl delete namespace victoria-logs-cluster
kubectl delete namespace victoria-logs-collector
kubectl delete namespace vmks
```


## Удаление компонентов

### Удаление всех компонентов и namespace

```bash
# Удаление Kafka Application
kubectl delete pod kafka-client -n kafka-app
kubectl delete -f schema-registry.yaml
kubectl delete secret schema-registry-credentials -n schema-registry

# Удаление Kafka кластера
kubectl delete kafkatopic -n kafka-cluster --all
kubectl delete kafkauser -n kafka-cluster --all
kubectl delete kafka kafka-cluster -n kafka-cluster

# Удаление Strimzi
helm uninstall strimzi-cluster-operator -n strimzi

# Удаление всех namespace
kubectl delete namespace kafka-app
kubectl delete namespace schema-registry
kubectl delete namespace kafka-cluster
kubectl delete namespace strimzi
```
