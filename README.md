# Тестирование Strimzi Kafka под высокой нагрузкой

Тестирование высоконагруженного кластера Apache Kafka, развернутого через оператор Strimzi в Kubernetes, с фокусом на развёртывание Kafka, топиков, пользователей и генерацию нагрузки.

Опциональные компоненты (Chaos Mesh, VictoriaLogs, victoria-logs-collector, VictoriaMetrics VM K8s Stack) вынесены в `ADDONS.md`.

## Strimzi

**Strimzi** — оператор Kubernetes для развертывания и управления Apache Kafka в Kubernetes. Предоставляет Custom Resource Definitions (CRDs) для управления Kafka-кластерами, топиками, пользователями и подключениями.

Данный проект использует **KRaft (Kafka Raft)** — новый механизм управления метаданными в Apache Kafka, который заменяет зависимость от ZooKeeper. KRaft упрощает архитектуру кластера, улучшает производительность и масштабируемость, а также снижает задержки при управлении метаданными.

### Установка Strimzi

```bash
# Namespace должен существовать заранее, если вы добавляете его в watchNamespaces
kubectl create namespace kafka-cluster --dry-run=client -o yaml | kubectl apply -f -

# Используем версию Strimzi 0.42.0 (совместима с Kafka 3.7.x и клиентом kafka-go в приложении)
helm upgrade --install strimzi-cluster-operator \
  oci://quay.io/strimzi-helm/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --set 'watchNamespaces={kafka-cluster}' \
  --wait \
  --version 0.42.0
```

Проверка установки:

```bash
kubectl get pods -n strimzi
```

### Развертывание Kafka кластера

После установки оператора Strimzi можно развернуть Kafka кластер в режиме KRaft.

В этом репозитории уже есть готовые манифесты:

- `kafka-cluster.yaml` — CR `Kafka` (с включёнными node pools через аннотацию `strimzi.io/node-pools: enabled` и KRaft через `strimzi.io/kraft: enabled`. Аутентификация отключена для совместимости с клиентом.)
- `kafka-nodepool.yaml` — CR `KafkaNodePool` (реплики/роли/хранилище)

Примечание: версия Strimzi из Helm-чарта в примере (`0.42.0`) поддерживает Kafka версии `3.7.x` (например `3.7.0`).

```bash
kubectl apply -f kafka-cluster.yaml
kubectl apply -f kafka-nodepool.yaml
```

Проверка статуса кластера:

```bash
# Проверка статуса Kafka кластера
kubectl get kafka -n kafka-cluster

# Проверка подов Kafka брокеров
kubectl get pods -n kafka-cluster -l strimzi.io/cluster=kafka-cluster

# Ожидание готовности кластера (статус Ready)
kubectl wait kafka/kafka-cluster -n kafka-cluster --for=condition=Ready --timeout=300s
```

После развертывания Kafka кластера адреса брокеров будут доступны через сервис:

- **Bootstrap сервер**: `kafka-cluster-kafka-bootstrap.kafka-cluster.svc.cluster.local:9092`

Для использования из других namespace:

```bash
# Получить адрес bootstrap сервера
kubectl get svc -n kafka-cluster kafka-cluster-kafka-bootstrap -o jsonpath='{.metadata.name}.{.metadata.namespace}.svc.cluster.local:{.spec.ports[?(@.name=="tcp-clients")].port}'; echo
```

### Доступ к Kafka извне кластера (port-forward)

Для тестов удобнее запускать Go-приложение **в кластере** с помощью Helm, так как `kubectl port-forward` имеет ограничения при работе с многоброкерными конфигурациями Kafka.

При необходимости доступа к Schema Registry:

```bash
# Schema Registry (HTTP) -> localhost:8081
kubectl -n schema-registry port-forward svc/schema-registry 8081:8081
```

### Создание Kafka топиков

Создайте Kafka топик через Strimzi KafkaTopic ресурс:

```bash
kubectl apply -f kafka-topic.yaml
```

Проверка создания топика:

```bash
# Проверка топиков
kubectl get kafkatopic -n kafka-cluster

# Детальная информация о топике
kubectl describe kafkatopic test-topic -n kafka-cluster
```

### Создание Kafka пользователей и секретов

(Опционально) Для аутентификации через SASL/SCRAM можно создать Kafka пользователя, но в данной конфигурации кластера аутентификация отключена.

#### Создание пользователя

```bash
kubectl apply -f kafka-user.yaml
kubectl wait kafkauser/myuser -n kafka-cluster --for=condition=Ready --timeout=120s
```

### Schema Registry (Karapace) для Avro

Go-приложение из этого репозитория использует Avro и Schema Registry API. Для удобства здесь добавлены готовые манифесты для **Karapace** — open-source реализации API Confluent Schema Registry (drop-in replacement): https://github.com/Aiven-Open/karapace

Karapace поднимается как обычный HTTP-сервис и хранит схемы в Kafka-топике `_schemas` (как и Confluent SR).

- `kafka-user-schema-registry.yaml` — KafkaUser с правами на `_schemas`
- `kafka-topic-schemas.yaml` — KafkaTopic для `_schemas` (важно при `min.insync.replicas: 2`)
- `schema-registry.yaml` — Service/Deployment для Karapace (`ghcr.io/aiven-open/karapace:latest`)

```bash
kubectl create namespace schema-registry --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f kafka-topic-schemas.yaml
kubectl wait kafkatopic/schemas-topic -n kafka-cluster --for=condition=Ready --timeout=120s

kubectl apply -f kafka-user-schema-registry.yaml
kubectl wait kafkauser/schema-registry -n kafka-cluster --for=condition=Ready --timeout=120s

kubectl apply -f schema-registry.yaml
kubectl rollout status deploy/schema-registry -n schema-registry --timeout=5m
kubectl get svc -n schema-registry schema-registry
```

### Если Schema Registry не поднимается: быстрая диагностика

- **`kubectl rollout status ...` уходит в timeout / pod не становится Ready**: чаще всего это либо неверные креды (не тот пароль/username), либо Kafka недоступна, либо security-настройки не совпадают (например, Kafka требует SASL, а Karapace запущен с PLAINTEXT). Диагностика:

```bash
kubectl get pods -n schema-registry
kubectl describe pod -n schema-registry -l app=schema-registry
kubectl logs -n schema-registry deploy/schema-registry --all-containers --tail=200
kubectl get events -n schema-registry --sort-by=.lastTimestamp | tail -n 30
```

## Producer App и Consumer App

**Producer App и Consumer App** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Используется для генерации нагрузки на кластер Kafka во время тестирования.

### Используемые библиотеки

- `segmentio/kafka-go` — клиент для работы с Kafka
- `riferrei/srclient` — клиент для Schema Registry API (совместим с Karapace)
- `goavro` (linkedin/goavro/v2) — работа с Avro схемами
- `xdg-go/scram` — SASL/SCRAM аутентификация (используется через kafka-go)

### Переменные окружения

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `MODE` | Режим работы: `producer` или `consumer` | `producer` |
| `KAFKA_BROKERS` | Список брокеров Kafka (через запятую) | `localhost:9092` |
| `KAFKA_TOPIC` | Название топика | `test-topic` |
| `SCHEMA_REGISTRY_URL` | URL Schema Registry | `http://localhost:8081` |
| `KAFKA_USERNAME` | Имя пользователя для SASL/SCRAM | - |
| `KAFKA_PASSWORD` | Пароль для SASL/SCRAM | - |
| `KAFKA_GROUP_ID` | Consumer Group ID (только для consumer) | `test-group` |

### Запуск Producer/Consumer в кластере используя Helm

Для запуска приложений в кластере используйте Helm charts из директории `helm`.

#### 1) Установить Producer (без аутентификации)
```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-cluster \
  --set kafka.username="" \
  --set kafka.password="" \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry.svc:8081"
```

#### 2) Установить Consumer (без аутентификации)
```bash
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-cluster \
  --set kafka.username="" \
  --set kafka.password="" \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry.svc:8081"
```

#### 3) Проверка логов
```bash
# Producer logs
kubectl logs -n kafka-cluster -l app.kubernetes.io/name=kafka-producer -f

# Consumer logs
kubectl logs -n kafka-cluster -l app.kubernetes.io/name=kafka-consumer -f
```
