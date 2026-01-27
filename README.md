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

### Доступ к Kafka извне кластера (ingress-nginx / port-forward)

Для тестов удобнее запускать Go-приложение **локально**, а доступ к Kafka (и при необходимости к Schema Registry) получать через:

- `ingress-nginx` как **TCP proxy** (когда нужен внешний адрес/порт для Kafka)
- `kubectl port-forward` (быстро и удобно для локальной отладки)

#### Вариант A: port-forward (рекомендуется для локальной отладки)

```bash
# Kafka bootstrap -> localhost:9092
kubectl -n kafka-cluster port-forward svc/kafka-cluster-kafka-bootstrap 9092:9092

# Schema Registry (HTTP) -> localhost:8081
kubectl -n schema-registry port-forward svc/schema-registry 8081:8081
```

Дальше Go-приложение можно запускать локально, обращаясь к Kafka через `localhost:9092` и Schema Registry через `http://localhost:8081`.

#### Вариант B: ingress-nginx как TCP proxy для Kafka (9092)

Важно: Kafka — это **не HTTP**, поэтому нужен TCP-проброс в `ingress-nginx` (через `tcp-services` ConfigMap).

1) Убедитесь, что `ingress-nginx` установлен и включён `tcp-services` ConfigMap:

```bash
# Пример для ingress-nginx Helm chart: включаем поддержку TCP services
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace \
  --set controller.extraArgs.tcp-services-configmap=ingress-nginx/tcp-services
```

2) Создайте ConfigMap с пробросом порта Kafka:

```bash
kubectl -n ingress-nginx create configmap tcp-services \
  --from-literal=9092="kafka-cluster/kafka-cluster-kafka-bootstrap:9092" \
  --dry-run=client -o yaml | kubectl apply -f -
```

3) Убедитесь, что у `ingress-nginx-controller` открыт порт 9092 (зависит от чарта/значений). После этого Kafka будет доступна на внешнем адресе ingress-nginx **по TCP 9092**.

> Примечание по безопасности: при `SASL_PLAINTEXT` логин/пароль идут без TLS. Для публикации наружу лучше использовать TLS/VPN/внутренний LB.

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
kubectl create secret generic schema-registry-credentials -n schema-registry \
  --from-literal=sasl.jaas.config="$(kubectl get secret schema-registry -n kafka-cluster -o go-template='{{index .data \"sasl.jaas.config\"}}' | base64 -d)" \
  --dry-run=client -o yaml | kubectl apply -f -

# Быстрая проверка (должна выводиться строка с ScramLoginModule ...)
kubectl get secret schema-registry-credentials -n schema-registry \
  -o go-template='{{index .data \"sasl.jaas.config\"}}' | base64 -d && echo

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

### Локальный запуск Go-приложения (рекомендуется)

Для быстрых тестов удобнее запускать producer/consumer **локально**, а Kafka/Schema Registry прокидывать через `port-forward` (или публиковать Kafka через `ingress-nginx`, см. выше).

Пример (через port-forward):

```bash
# В отдельных терминалах держим port-forward:
# kubectl -n kafka-cluster port-forward svc/kafka-cluster-kafka-bootstrap 9092:9092
# kubectl -n schema-registry port-forward svc/schema-registry 8081:8081

PASS=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
MODE=producer \
KAFKA_BROKERS=localhost:9092 \
KAFKA_TOPIC=test-topic \
SCHEMA_REGISTRY_URL=http://localhost:8081 \
KAFKA_USERNAME=myuser \
KAFKA_PASSWORD="$PASS" \
go run .
```


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

Примечание по отладке:

- Если в логах `kafka-producer` видно `Topic Authorization Failed`, проверьте ACL в `kafka-user.yaml` и пересоздайте `KafkaUser` (и secret в `kafka-apps`).
- Если `kafka-producer`/`kafka-consumer` пишут `Unknown Topic Or Partition`, а `KafkaTopic` при этом `Ready`, можно быстро проверить доступность топика через Kafka CLI внутри broker pod:

```bash
PASS=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
kubectl exec -n kafka-cluster kafka-cluster-mixed-0 -- bash -lc "cat > /tmp/client.properties <<'EOF'
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-512
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username=\"myuser\" password=\"$PASS\";
EOF
/opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --command-config /tmp/client.properties --describe --topic test-topic"
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
