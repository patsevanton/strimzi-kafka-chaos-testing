# Kafka Producer Helm Chart

Helm чарт для развертывания Go приложения в режиме producer (отправка данных в Kafka).

## Установка

```bash
helm install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  -f helm/kafka-producer/values.yaml
```

## Настройка

Основные параметры в `values.yaml`:

### Kafka настройки
- `kafka.brokers` - список брокеров Kafka (через запятую)
- `kafka.topic` - название топика
- `kafka.producerIntervalMs` - интервал между сообщениями (ms). 100 = 10 msg/s на под. Уменьшить для большей нагрузки (50→20 msg/s, 20→50 msg/s).

### Schema Registry
- `schemaRegistry.url` - URL Schema Registry API (Karapace/Confluent-compatible)

### Пример values.yaml для Strimzi

```yaml
replicaCount: 1

image:
  repository: kafka-app
  tag: "latest"

kafka:
  brokers: "kafka-cluster-kafka-bootstrap.kafka-cluster.svc.cluster.local:9092"
  topic: "test-topic"

schemaRegistry:
  url: "http://schema-registry.schema-registry:8081"
```

## Обновление

```bash
helm upgrade kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  -f helm/kafka-producer/values.yaml
```

## Удаление

```bash
helm uninstall kafka-producer --namespace kafka-producer
```
