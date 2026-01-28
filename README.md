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

helm upgrade --install strimzi-cluster-operator \
  oci://quay.io/strimzi-helm/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --set 'watchNamespaces={kafka-cluster}' \
  --wait \
  --version 0.50.0
```

Проверка установки:

```bash
kubectl get pods -n strimzi
```

### Развертывание Kafka кластера

После установки оператора Strimzi можно развернуть Kafka кластер в режиме KRaft.

В этом репозитории уже есть готовые манифесты:

- `kafka-cluster.yaml` — CR `Kafka` (с включёнными node pools через аннотацию `strimzi.io/node-pools: enabled`)
- `kafka-nodepool.yaml` — CR `KafkaNodePool` (реплики/роли/хранилище)

Примечание: версия Strimzi из Helm-чарта в примере (`0.50.0`) поддерживает Kafka версии `4.x` (например `4.1.1`).

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

Для тестов удобнее запускать Go-приложение **локально**, а доступ к Kafka (и при необходимости к Schema Registry) получать через `kubectl port-forward`.

#### Port-forward (рекомендуется для локальной отладки)

```bash
# Kafka bootstrap -> localhost:9092
kubectl -n kafka-cluster port-forward svc/kafka-cluster-kafka-bootstrap 9092:9092

# Schema Registry (HTTP) -> localhost:8081
kubectl -n schema-registry port-forward svc/schema-registry 8081:8081
```

Дальше Go-приложение можно запускать локально, обращаясь к Kafka через `localhost:9092` и Schema Registry через `http://localhost:8081`.

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

Для аутентификации через SASL/SCRAM создайте Kafka пользователя. Strimzi автоматически создаст секрет с credentials.

#### Создание пользователя

```bash
kubectl apply -f kafka-user.yaml
kubectl wait kafkauser/myuser -n kafka-cluster --for=condition=Ready --timeout=120s
```

После создания KafkaUser, Strimzi автоматически создаст секрет с именем `myuser` в том же namespace, содержащий:
- `password` — пароль пользователя

#### Получение credentials из секрета

```bash
# Получить имя пользователя (обычно совпадает с именем KafkaUser)
USERNAME=myuser

# Получить пароль из секрета
PASSWORD=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
```

#### Использование credentials в приложениях

Strimzi создаёт секрет `myuser` в namespace `kafka-cluster`. Для приложений в других namespace обычно проще **считать пароль** из этого секрета и передать его как переменную окружения/helm value, либо скопировать secret в нужный namespace отдельной командой (зависит от ваших практик безопасности).

#### Проверка пользователей и секретов

```bash
# Проверка Kafka пользователей
kubectl get kafkauser -n kafka-cluster

# Проверка секретов
kubectl get secret myuser -n kafka-cluster

# Просмотр содержимого секрета (без пароля)
kubectl describe secret myuser -n kafka-cluster

# Получение пароля из секрета
kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d && echo
```

## Schema Registry (Karapace) для Avro

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

# Создать Secret с учётными данными для SASL/SCRAM в namespace schema-registry
# (username = имя KafkaUser, password берём из секрета Strimzi)
kubectl create secret generic schema-registry-credentials -n schema-registry \
  --from-literal=sasl_plain_username="schema-registry" \
  --from-literal=sasl_plain_password="$(kubectl get secret schema-registry -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)" \
  --dry-run=client -o yaml | kubectl apply -f -

# Быстрая проверка (должны выводиться username и password)
kubectl get secret schema-registry-credentials -n schema-registry -o jsonpath='{.data.sasl_plain_username}' | base64 -d && echo
kubectl get secret schema-registry-credentials -n schema-registry -o jsonpath='{.data.sasl_plain_password}' | base64 -d && echo

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

### Запуск Producer/Consumer локально используя port-forward

Условия:

- есть доступ в Kubernetes через `kubectl`
- локально установлен Go (`go`)

#### 1) Прокинуть Kafka и Schema Registry на localhost

Держите эти команды запущенными в отдельных терминалах:

```bash
# Kafka bootstrap -> localhost:9092
kubectl -n kafka-cluster port-forward svc/kafka-cluster-kafka-bootstrap 9092:9092

# Schema Registry (HTTP) -> localhost:8081
kubectl -n schema-registry port-forward svc/schema-registry 8081:8081
```

#### 2) (Опционально) Получить пароль для SASL/SCRAM из секрета Strimzi

Если Kafka у вас **без** аутентификации, шаг можно пропустить (не задавайте `KAFKA_USERNAME`/`KAFKA_PASSWORD`).

```bash
export KAFKA_USERNAME=myuser
export KAFKA_PASSWORD="$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)"
```

#### 3) Запустить producer локально

```bash
MODE=producer \
KAFKA_BROKERS=localhost:9092 \
KAFKA_TOPIC=test-topic \
SCHEMA_REGISTRY_URL=http://localhost:8081 \
KAFKA_USERNAME="${KAFKA_USERNAME:-}" \
KAFKA_PASSWORD="${KAFKA_PASSWORD:-}" \
go run .
```

#### 4) Запустить consumer локально

```bash
MODE=consumer \
KAFKA_BROKERS=localhost:9092 \
KAFKA_TOPIC=test-topic \
KAFKA_GROUP_ID=test-group \
SCHEMA_REGISTRY_URL=http://localhost:8081 \
KAFKA_USERNAME="${KAFKA_USERNAME:-}" \
KAFKA_PASSWORD="${KAFKA_PASSWORD:-}" \
go run .
```

