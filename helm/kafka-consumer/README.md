# Kafka Consumer Helm Chart

Helm чарт для развертывания Go приложения в режиме consumer (получение данных из Kafka).

## Установка

```bash
helm install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-apps \
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
- `schemaRegistry.url` - URL Schema Registry

### Безопасность

Для использования секретов вместо plain text паролей:

```yaml
secrets:
  create: true
  username: "myuser"
  password: "mypassword"
```

### Пример values.yaml для Strimzi

```yaml
replicaCount: 1

image:
  repository: kafka-app
  tag: "latest"

kafka:
  brokers: "my-cluster-kafka-bootstrap:9092"
  topic: "test-topic"
  groupId: "my-consumer-group"
  username: "myuser"
  password: "mypassword"

schemaRegistry:
  url: "http://schema-registry:8081"

secrets:
  create: true
  username: "myuser"
  password: "mypassword"
```

## Обновление

```bash
helm upgrade kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-apps \
  -f helm/kafka-consumer/values.yaml
```

## Удаление

```bash
helm uninstall kafka-consumer --namespace kafka-apps
```
