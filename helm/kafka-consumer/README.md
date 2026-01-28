# Kafka Consumer Helm Chart

Helm чарт для развертывания Go приложения в режиме consumer (получение данных из Kafka).

## Установка

```bash
helm install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-consumer \
  --create-namespace \
  -f helm/kafka-consumer/values.yaml
```

## Настройка

Основные параметры в `values.yaml`:

### Kafka настройки
- `kafka.brokers` - список брокеров Kafka (через запятую)
- `kafka.topic` - название топика
- `kafka.groupId` - Consumer Group ID
- `kafka.username` - имя пользователя для SASL/SCRAM (опционально)
- `kafka.password` - пароль для SASL/SCRAM (опционально)

### Schema Registry
- `schemaRegistry.url` - URL Schema Registry API (Karapace/Confluent-compatible)

### Безопасность

Для использования секретов вместо plain text паролей (рекомендуется):

```yaml
secrets:
  create: true
  username: "myuser"
  password: "mypassword"
```

Также можно использовать **уже существующий** secret в namespace релиза:
```bash
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-consumer \
  --set secrets.name=kafka-app-credentials
```

### Пример values.yaml для Strimzi

```yaml
replicaCount: 1

image:
  repository: kafka-app
  tag: "latest"

kafka:
  brokers: "kafka-cluster-kafka-bootstrap.kafka-cluster:9092"
  topic: "test-topic"
  groupId: "my-consumer-group"
  username: "myuser"
  password: "mypassword"

schemaRegistry:
  url: "http://schema-registry.schema-registry:8081"

secrets:
  create: true
  username: "myuser"
  password: "mypassword"
```

## Обновление

```bash
helm upgrade kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-consumer \
  -f helm/kafka-consumer/values.yaml
```

## Удаление

```bash
helm uninstall kafka-consumer --namespace kafka-consumer
```
