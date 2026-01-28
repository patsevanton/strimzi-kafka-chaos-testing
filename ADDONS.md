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

### Формат сообщений

Приложение использует Avro схему для сериализации сообщений:

```json
{
  "type": "record",
  "name": "Message",
  "namespace": "com.example",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "timestamp", "type": "long", "logicalType": "timestamp-millis"},
    {"name": "data", "type": "string"}
  ]
}
```

Producer отправляет сообщения каждую секунду с автоматически увеличивающимся ID. Consumer читает сообщения из указанного топика и выводит их в лог.



## Удаление add-ons (Chaos Mesh / Observability)

Ниже — **удаление Helm-релизов** этих add-ons и (опционально) их namespace.

### Удаление Helm-релизов

```bash
# Chaos Mesh (namespace: chaos-mesh)
helm uninstall chaos-mesh -n chaos-mesh

# VictoriaLogs (namespace: victoria-logs-cluster)
helm uninstall victoria-logs-cluster -n victoria-logs-cluster

# victoria-logs-collector (namespace: victoria-logs-collector)
helm uninstall victoria-logs-collector -n victoria-logs-collector

# VictoriaMetrics (VM K8s Stack) (namespace: vmks)
helm uninstall vmks -n vmks
```

### Удаление namespace add-ons (опционально)

Удаляйте namespace **только если** вы создавали их под эти add-ons и не используете больше ни для чего.

```bash
kubectl delete namespace chaos-mesh
kubectl delete namespace victoria-logs-cluster
kubectl delete namespace victoria-logs-collector
kubectl delete namespace vmks
```

## Удаление основной части (Strimzi / Kafka / Schema Registry / приложения)

Этот блок описывает **что именно удаляется**:
- **Producer/Consumer приложения**: Helm-релизы `kafka-producer` и `kafka-consumer` в namespace релиза.
- **Schema Registry (Karapace)**: `Service/Deployment` из `schema-registry.yaml` в namespace `schema-registry` + секрет `schema-registry` (скопированный из `kafka-cluster`).
- **Kafka (Strimzi CRs)**: ресурсы `Kafka`, `KafkaNodePool`, `KafkaTopic`, `KafkaUser` в namespace `kafka-cluster`.
- **Strimzi operator**: Helm-релиз `strimzi-cluster-operator` в namespace `strimzi`.

### Удаление приложений (Helm)

```bash
helm uninstall kafka-producer -n kafka-producer
helm uninstall kafka-consumer -n kafka-consumer
```

### Удаление Schema Registry (Karapace)

```bash
# Удалить Service/Deployment (namespace уже указан в манифесте)
kubectl delete -f schema-registry.yaml

# Удалить секрет, который вы копировали в namespace schema-registry
kubectl delete secret schema-registry -n schema-registry

# Удалить KafkaUser/топик для Schema Registry в kafka-cluster
kubectl delete -f kafka-user-schema-registry.yaml
kubectl delete -f kafka-topic-schemas.yaml
```

### Удаление Kafka ресурсов (kafka-cluster)

```bash
# KafkaTopic / KafkaUser из этого репозитория
kubectl delete -f kafka-topic.yaml
kubectl delete -f kafka-user.yaml

# Kafka кластер и node pool
kubectl delete -f kafka-cluster.yaml
kubectl delete -f kafka-nodepool.yaml
```

### Удаление Strimzi operator

```bash
helm uninstall strimzi-cluster-operator -n strimzi
```

### Удаление namespace (опционально)

```bash
# Удаляйте только если namespace’ы не используются ничем другим
# Если вы ставили приложения в отдельные namespace:
kubectl delete namespace kafka-producer
kubectl delete namespace kafka-consumer

# Namespace’ы основной установки из этого репозитория:
kubectl delete namespace schema-registry
kubectl delete namespace kafka-cluster
kubectl delete namespace strimzi
```
