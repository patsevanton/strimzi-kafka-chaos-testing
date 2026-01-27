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

## Schema Registry (Confluent) для Avro

Go-приложение из этого репозитория использует Avro и Schema Registry. Для удобства здесь добавлены готовые манифесты:

- `kafka-user-schema-registry.yaml` — KafkaUser с правами на `_schemas`
- `schema-registry.yaml` — Service/Deployment для `confluentinc/cp-schema-registry`

Важно:

- В `schema-registry.yaml` включено `enableServiceLinks: false`, иначе Kubernetes добавляет переменную окружения `SCHEMA_REGISTRY_PORT`, и скрипт старта контейнера завершится с ошибкой.
- `SCHEMA_REGISTRY_HOST_NAME` берётся из `status.podIP` (уникально для каждого pod). Это предотвращает ошибку Confluent Schema Registry про “duplicate URLs” при рестартах/масштабировании.
- В `schema-registry.yaml` используется стратегия `Recreate`, чтобы при обновлениях не было одновременно 2 pod'ов Schema Registry (что часто ломает leader election/coordination в тестовых окружениях).

```bash
kubectl create namespace schema-registry --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f kafka-user-schema-registry.yaml
kubectl wait kafkauser/schema-registry -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать sasl.jaas.config из секрета Strimzi в Secret в namespace schema-registry
# Важно: используем --from-literal (а не process substitution), чтобы значение точно не оказалось пустым.
JAAS=$(kubectl get secret schema-registry -n kafka-cluster -o jsonpath='{.data.sasl\\.jaas\\.config}' | base64 -d)
kubectl create secret generic schema-registry-credentials -n schema-registry \
  --from-literal=sasl.jaas.config="$JAAS" \
  --dry-run=client -o yaml | kubectl apply -f -

# Быстрая проверка (должна выводиться строка с ScramLoginModule ...)
kubectl get secret schema-registry-credentials -n schema-registry \
  -o jsonpath='{.data.sasl\\.jaas\\.config}' | base64 -d && echo

kubectl apply -f schema-registry.yaml
kubectl rollout status deploy/schema-registry -n schema-registry --timeout=5m
kubectl get svc -n schema-registry schema-registry
```

## Producer App и Consumer App

**Producer App и Consumer App** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Используется для генерации нагрузки на кластер Kafka во время тестирования.

### Используемые библиотеки

- `segmentio/kafka-go` — клиент для работы с Kafka
- `riferrei/srclient` — клиент для Schema Registry
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


### Запуск Producer/Consumer в Kubernetes через Helm

В репозитории есть чарты `helm/kafka-producer` и `helm/kafka-consumer`. Они используют переменные окружения приложения:
- `KAFKA_BROKERS` → `kafka-cluster-kafka-bootstrap.kafka-cluster:9092`
- `SCHEMA_REGISTRY_URL` → `http://schema-registry.schema-registry:8081`

Важно: secret `myuser` создаётся Strimzi в namespace `kafka-cluster`, поэтому для приложений в `kafka-apps` нужно создать отдельный secret с тем же логином/паролем.

```bash
kubectl create namespace kafka-apps --dry-run=client -o yaml | kubectl apply -f -

# Скопировать пароль из секрета Strimzi и создать secret в namespace приложений
PASS=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
kubectl create secret generic kafka-app-credentials -n kafka-apps \
  --from-literal=username=myuser \
  --from-literal=password="$PASS" \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-apps \
  --set secrets.name=kafka-app-credentials

helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-apps \
  --set secrets.name=kafka-app-credentials

kubectl rollout status deploy/kafka-producer -n kafka-apps --timeout=5m
kubectl rollout status deploy/kafka-consumer -n kafka-apps --timeout=5m

kubectl logs -n kafka-apps deploy/kafka-producer --tail=50
kubectl logs -n kafka-apps deploy/kafka-consumer --tail=50
```

Проверьте **consumer group** (состояние и lag):

```bash
# group id по умолчанию: test-group (см. KAFKA_GROUP_ID)
kubectl exec -n kafka-cluster -it kafka-cluster-kafka-0 -- \
  bin/kafka-consumer-groups.sh \
  --bootstrap-server kafka-cluster-kafka-bootstrap.kafka-cluster:9092 \
  --describe --group test-group
```

Примечание: consumer ожидает Avro-сообщения (пишет producer из этого приложения).

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
