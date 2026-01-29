# Тестирование Strimzi Kafka под высокой нагрузкой

Проект для тестирования отказоустойчивости и производительности высоконагруженного кластера Apache Kafka в Kubernetes. Кластер разворачивается с помощью оператора Strimzi. Включает инструменты для хаос-тестирования через Chaos Mesh, мониторинг через VictoriaMetrics, Schema Registry для управления схемами данных, Kafka UI для веб-интерфейса управления кластером, а также примеры producer и consumer приложений на Go.

## Strimzi

**Strimzi** — оператор Kubernetes для развертывания и управления Apache Kafka в Kubernetes. Предоставляет Custom Resource Definitions (CRDs) для управления Kafka-кластерами, топиками, пользователями и подключениями.

В Данном тестировании Kafka использует **KRaft (Kafka Raft)** — новый механизм управления метаданными в Apache Kafka, который заменяет зависимость от ZooKeeper. KRaft упрощает архитектуру кластера, улучшает производительность и масштабируемость, а также снижает задержки при управлении метаданными.

### Установка Strimzi

```bash
# Namespace должен существовать заранее, если вы добавляете его в watchNamespaces
kubectl create namespace kafka-cluster --dry-run=client -o yaml | kubectl apply -f -

# Используем версию Strimzi 0.50.0 (совместима с Kafka 4.x и клиентом kafka-go в приложении)
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

- `kafka-cluster.yaml` — CR `Kafka` (с включёнными node pools через аннотацию `strimzi.io/node-pools: enabled` и KRaft через `strimzi.io/kraft: enabled`. **Включена SASL/SCRAM-SHA-512 аутентификация и ACL авторизация.**)
- `kafka-nodepool.yaml` — CR `KafkaNodePool` (реплики/роли/хранилище)

Примечание: версия Strimzi из Helm-чарта в примере (`0.50.0`) поддерживает Kafka версии `4.x` (например `4.1.1`).

Важно: при включённых node pools (`strimzi.io/node-pools: enabled`) лучше сначала создать `KafkaNodePool`, а затем `Kafka`.
Иначе оператор Strimzi может логировать ошибку вида `KafkaNodePools are enabled, but no KafkaNodePools found...` до момента создания node pool.

```bash
kubectl apply -f kafka-nodepool.yaml
kubectl apply -f kafka-cluster.yaml
```

Если PVC остаются в `Pending` с ошибкой `ResourceExhausted`, уменьшите размер дисков в `kafka-nodepool.yaml`
или укажите подходящий `storageClass` для вашего кластера.

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

Для работы с Kafka кластером с включённой SASL/SCRAM аутентификацией необходимо создать KafkaUser ресурсы. Strimzi автоматически генерирует секреты с credentials для каждого пользователя.

#### Создание пользователя для приложения

```bash
kubectl apply -f kafka-user.yaml
kubectl wait kafkauser/myuser -n kafka-cluster --for=condition=Ready --timeout=120s
```

После создания пользователя Strimzi создаёт секрет с тем же именем (`myuser`), содержащий:
- `password` — сгенерированный пароль для SCRAM аутентификации
- `sasl.jaas.config` — полная JAAS конфигурация

**Важно**: Имя пользователя (username) равно имени KafkaUser/секрета, т.е. `myuser`.

Проверка секрета:

```bash
# Посмотреть пароль (только для отладки; не публикуйте этот вывод)
kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d; echo

# Посмотреть JAAS config (только для отладки; не публикуйте этот вывод)
kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.sasl\.jaas\.config}' | base64 -d; echo
```

### Schema Registry (Karapace) для Avro

Go-приложение из этого репозитория использует Avro и Schema Registry API. Для удобства здесь добавлены готовые манифесты для **Karapace** — open-source реализации API Confluent Schema Registry (drop-in replacement): https://github.com/Aiven-Open/karapace

Karapace поднимается как обычный HTTP-сервис и хранит схемы в Kafka-топике `_schemas` (как и Confluent SR).

- `kafka-topic-schemas.yaml` — KafkaTopic для `_schemas` (важно при `min.insync.replicas: 2`)
- `kafka-user-schema-registry.yaml` — KafkaUser для Schema Registry с ACL для топика `_schemas`
- `schema-registry.yaml` — Service/Deployment для Karapace (`ghcr.io/aiven-open/karapace:5.0.3`). **Настроен на SASL/SCRAM-SHA-512 аутентификацию.**

```bash
kubectl create namespace schema-registry --dry-run=client -o yaml | kubectl apply -f -

# Создать топик для схем
kubectl apply -f kafka-topic-schemas.yaml
kubectl wait kafkatopic/schemas-topic -n kafka-cluster --for=condition=Ready --timeout=120s

# Создать пользователя для Schema Registry (обязательно для SASL аутентификации)
kubectl apply -f kafka-user-schema-registry.yaml
kubectl wait kafkauser/schema-registry -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать секрет в namespace schema-registry (Strimzi создаёт секрет в kafka-cluster)
kubectl get secret schema-registry -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  kubectl apply -n schema-registry -f -

# Развернуть Schema Registry
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

# Проверить что секрет скопирован
kubectl get secret schema-registry -n schema-registry
```

## Producer App и Consumer App

**Producer App и Consumer App** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Используется для генерации нагрузки на кластер Kafka во время тестирования.

### Используемые библиотеки

- `segmentio/kafka-go` — клиент для работы с Kafka
- `riferrei/srclient` — клиент для Schema Registry API (совместим с Karapace)
- `goavro` (linkedin/goavro/v2) — работа с Avro схемами
- `xdg-go/scram` — SASL/SCRAM аутентификация (используется через kafka-go)

### Сборка и публикация Docker образа

Go-код в `main.go` можно изменять под свои нужды. После внесения изменений соберите и опубликуйте Docker образ:

```bash
# Сборка образа (используйте podman или docker)
podman build -t docker.io/antonpatsev/strimzi-kafka-chaos-testing:3.0.0 .

# Публикация в Docker Hub
podman push docker.io/antonpatsev/strimzi-kafka-chaos-testing:3.0.0
```

После публикации обновите версию образа в Helm values или передайте через `--set`:

```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set image.repository="antonpatsev/strimzi-kafka-chaos-testing" \
  --set image.tag="3.0.0"
```

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

**Важно**: Перед запуском убедитесь, что KafkaUser `myuser` создан и готов (см. раздел "Создание Kafka пользователей").

Также важно: **Strimzi создаёт secret `myuser` в namespace `kafka-cluster`**, а Kubernetes secrets **не доступны между namespace**.
Если вы запускаете приложения в отдельных namespace, сначала скопируйте secret в каждый namespace приложения:

```bash
# Namespaces для приложений
kubectl create namespace kafka-producer --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace kafka-consumer --dry-run=client -o yaml | kubectl apply -f -

# Скопировать secret myuser из kafka-cluster → kafka-producer
kubectl get secret myuser -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  kubectl apply -n kafka-producer -f -

# Скопировать secret myuser из kafka-cluster → kafka-consumer
kubectl get secret myuser -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  kubectl apply -n kafka-consumer -f -
```

#### 1) Установить Producer (с аутентификацией через Strimzi Secret)
```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry.svc:8081" \
  --set secrets.name="myuser"
```

#### 2) Установить Consumer (с аутентификацией через Strimzi Secret)
```bash
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-consumer \
  --create-namespace \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry.svc:8081" \
  --set secrets.name="myuser"
```

Helm charts автоматически берут `username` и `password` из указанного секрета (`myuser`), который был создан Strimzi при создании KafkaUser.

#### Альтернатива: передать credentials напрямую (не рекомендуется для production)
```bash
# Получить пароль из секрета Strimzi
KAFKA_PASSWORD=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)

helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster:9092" \
  --set kafka.username="myuser" \
  --set kafka.password="$KAFKA_PASSWORD" \
  --set schemaRegistry.url="http://schema-registry.schema-registry.svc:8081"
```

#### 3) Проверка логов
```bash
# Producer logs
kubectl logs -n kafka-producer -l app.kubernetes.io/name=kafka-producer -f

# Consumer logs
kubectl logs -n kafka-consumer -l app.kubernetes.io/name=kafka-consumer -f
```

## Kafka UI, Chaos Mesh и Observability

### Kafka UI (Kafbat UI)

**Kafka UI** — веб-интерфейс с открытым исходным кодом для управления и мониторинга Apache Kafka кластеров. Позволяет просматривать топики, сообщения, consumer groups, брокеры и конфигурации кластера через удобный графический интерфейс.

GitHub: https://github.com/kafbat/kafka-ui

Основные возможности:
- Просмотр и управление топиками (создание, удаление, конфигурация)
- Просмотр сообщений в различных форматах (JSON, Avro, Protobuf)
- Мониторинг consumer groups и их лага
- Информация о брокерах и состоянии кластера
- Интеграция с Schema Registry
- RBAC и аутентификация

#### Установка Kafka UI

```bash
# Создать пользователя для Kafka UI
kubectl apply -f kafka-user-ui.yaml
kubectl wait kafkauser/kafka-ui-user -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать секрет в namespace kafka-ui
kubectl create namespace kafka-ui --dry-run=client -o yaml | kubectl apply -f -

# Создать секрет с credentials для Kafka UI
kubectl get secret kafka-ui-user -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  jq '.data.username = ("kafka-ui-user" | @base64)' | \
  kubectl apply -n kafka-ui -f -

# Развернуть Kafka UI
kubectl apply -f kafka-ui.yaml
kubectl rollout status deploy/kafka-ui -n kafka-ui --timeout=5m
```

#### Проверка установки

```bash
# Проверить статус пода
kubectl get pods -n kafka-ui

# Проверить логи
kubectl logs -n kafka-ui -l app=kafka-ui --tail=100

# Получить сервис
kubectl get svc -n kafka-ui kafka-ui
```

#### Доступ к Kafka UI

Для локального доступа через port-forward:

```bash
kubectl port-forward -n kafka-ui svc/kafka-ui 8080:8080
```

После этого откройте в браузере: http://localhost:8080

Для production-окружения рекомендуется настроить Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kafka-ui
  namespace: kafka-ui
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "50m"
spec:
  ingressClassName: nginx
  rules:
    - host: kafka-ui.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kafka-ui
                port:
                  number: 8080
```

#### Диагностика проблем Kafka UI

Если Kafka UI не запускается:

```bash
# Проверить события
kubectl get events -n kafka-ui --sort-by=.lastTimestamp | tail -n 20

# Проверить логи
kubectl logs -n kafka-ui deploy/kafka-ui --all-containers --tail=200

# Проверить секрет
kubectl get secret kafka-ui-user -n kafka-ui -o yaml

# Проверить подключение к Kafka (из пода kafka-ui)
kubectl exec -n kafka-ui deploy/kafka-ui -- env | grep KAFKA
```

### Chaos Mesh

**Chaos Mesh** — платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

#### Установка Chaos Mesh

Для доступа к Dashboard через `ingress-nginx` используйте файл `chaos-mesh-values.yaml` из репозитория.

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh \
  --create-namespace \
  -f chaos-mesh-values.yaml \
  --version 2.8.1 \
  --wait
```

Проверка установки:

```bash
kubectl get pods -n chaos-mesh
```

### Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из компонентов кластера Kafka и приложений.

#### VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

##### Установка: Cluster

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

#### victoria-logs-collector

`victoria-logs-collector` — Helm-чарт от VictoriaMetrics, разворачивающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилище.

##### Установка

Для установки используйте `victorialogs-collector-values.yaml` из репозитория.

```bash
helm upgrade --install victoria-logs-collector \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-collector \
  --namespace victoria-logs-collector \
  --create-namespace \
  --wait \
  --version 0.2.8 \
  --timeout 15m \
  -f victorialogs-collector-values.yaml
```

#### VictoriaMetrics (VM K8s Stack)

`victoria-metrics-k8s-stack` — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

##### Установка

Для установки используйте `vmks-values.yaml` из репозитория.

```bash
helm upgrade --install vmks \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-metrics-k8s-stack \
  --namespace vmks \
  --create-namespace \
  --wait \
  --version 0.68.0 \
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

## Удаление (Helm / приложения / Strimzi / Kafka)

Инструкции по удалению вынесены в отдельный файл: `uninstall.md`.
