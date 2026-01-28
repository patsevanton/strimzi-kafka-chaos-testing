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
# Удаляйте только если namespace'ы не используются ничем другим
# Если вы ставили приложения в отдельные namespace:
kubectl delete namespace kafka-producer
kubectl delete namespace kafka-consumer

# Namespace'ы основной установки из этого репозитория:
kubectl delete namespace schema-registry
kubectl delete namespace kafka-cluster
kubectl delete namespace strimzi
```
