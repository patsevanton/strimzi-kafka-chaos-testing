# Тестирование Strimzi Kafka под высокой нагрузкой с помощью Chaos Mesh

Тестирование высоконагруженного кластера Apache Kafka, развернутого через оператор Strimzi в Kubernetes, с использованием Chaos Mesh для внедрения различных типов сбоев и проверки отказоустойчивости системы под нагрузкой.

Для обеспечения наблюдаемости во время тестирования используется observability stack на базе VictoriaLogs и VictoriaMetrics, который позволяет отслеживать состояние системы, анализировать логи и метрики в реальном времени.

## Strimzi

**Strimzi** — оператор Kubernetes для развертывания и управления Apache Kafka в Kubernetes. Предоставляет Custom Resource Definitions (CRDs) для управления Kafka-кластерами, топиками, пользователями и подключениями.

Данный проект использует **KRaft (Kafka Raft)** — новый механизм управления метаданными в Apache Kafka, который заменяет зависимость от ZooKeeper. KRaft упрощает архитектуру кластера, улучшает производительность и масштабируемость, а также снижает задержки при управлении метаданными.

### Установка Strimzi

```bash
# Namespace должен существовать заранее, если вы добавляете его в watchNamespaces
kubectl create namespace kafka-cluster

helm upgrade --install strimzi-cluster-operator \
  oci://quay.io/strimzi-helm/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --set 'watchNamespaces={strimzi,kafka-cluster}' \
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

## Schema Registry (Confluent) для Avro (опционально)

Go-приложение из этого репозитория использует Avro и Schema Registry. Для удобства здесь добавлены готовые манифесты:

- `kafka-user-schema-registry.yaml` — KafkaUser с правами на `_schemas`
- `schema-registry.yaml` — Service/Deployment для `confluentinc/cp-schema-registry`

Важно: для Confluent image в Kubernetes в `schema-registry.yaml` включено `enableServiceLinks: false`, иначе Kubernetes добавляет переменную окружения `SCHEMA_REGISTRY_PORT`, и скрипт старта контейнера завершится с ошибкой.

```bash
kubectl create namespace kafka-app --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f kafka-user-schema-registry.yaml
kubectl wait kafkauser/schema-registry -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать sasl.jaas.config из секрета Strimzi в Secret в namespace kafka-app
kubectl create secret generic schema-registry-credentials -n kafka-app \
  --from-file=sasl.jaas.config=<(kubectl get secret schema-registry -n kafka-cluster -o jsonpath='{.data.sasl\\.jaas\\.config}' | base64 -d) \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f schema-registry.yaml
kubectl rollout status deploy/schema-registry -n kafka-app --timeout=5m
kubectl get svc -n kafka-app schema-registry
```

## Chaos Mesh

**Chaos Mesh** — облачная платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

### Установка Chaos Mesh

Для доступа к Dashboard через ingress-nginx используйте файл values:

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

# Создайте файл chaos-mesh-values.yaml
cat > chaos-mesh-values.yaml <<EOF
chaosDaemon:
  runtime: containerd
  socketPath: /run/containerd/containerd.sock
dashboard:
  ingress:
    enabled: true
    ingressClassName: nginx
    hosts:
      - host: chaos-dashboard.apatsev.org.ru
        paths:
          - path: /
            pathType: Prefix
    annotations: {}
EOF

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

Откройте в браузере: `https://chaos-dashboard.apatsev.org.ru`

## Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из всех компонентов кластера Kafka и приложений, работающих с ним.

### VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

#### Установка: Cluster

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

Пример `victorialogs-cluster-values.yaml`:

```yaml
vlselect:
  ingress:
    enabled: true
    hosts:
      - name: victorialogs.apatsev.org.ru
        path:
          - /
        port: http
    ingressClassName: nginx
    annotations: {}
```

### victoria-logs-collector

`victoria-logs-collector` — это Helm-чарт от VictoriaMetrics, развертывающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилища.

#### Установка

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

Пример `victorialogs-collector-values.yaml`:

```yaml
# Настройки отправки логов во внешнее хранилище (VictoriaLogs)
remoteWrite:
  - url: http://victoria-logs-cluster-vlinsert.victoria-logs-cluster:9481
    headers:
      # Поля, которые будут проигнорированы и не сохранены в VictoriaLogs
      # Полезно для уменьшения объёма данных и шума
      VL-Ignore-Fields:
        - kubernetes.container_id # уникальный ID контейнера, часто меняется и не несет ценной информации
        - kubernetes.pod_ip # IP адрес пода, динамический и редко полезный для анализа логов
        - kubernetes.pod_labels.pod-template-hash # хэш шаблона Deployment ReplicaSet, используется для идентификации реплик, но избыточен

# Настройки collector: определяют, как извлекать сообщение лога из входных данных.
collector:
  # msgField: список полей, из которых извлекается основное сообщение лога (_msg в VictoriaLogs)
  msgField:
    - message
    - msg
```

### VictoriaMetrics (VM K8s Stack)

`victoria-metrics-k8s-stack` — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

#### Установка

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

Пример `vmks-values.yaml`:

```yaml
grafana:
  plugins:
    - victoriametrics-logs-datasource
  ingress:
    ingressClassName: nginx
    enabled: true
    hosts:
      - grafana.apatsev.org.ru
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "false"
defaultDatasources:
  extra:
    - name: victoriametrics-logs
      access: proxy
      type: victoriametrics-logs-datasource
      url: http://victoria-logs-cluster-vlselect.victoria-logs-cluster.svc.cluster.local:9471
      jsonData:
        maxLines: 1000
      version: 1
defaultRules:
  groups:
    etcd:
      create: false
kube-state-metrics:
  metricLabelsAllowlist:
    - pods=[*]
vmsingle:
  enabled: false
vmcluster:
  enabled: true
  ingress:
    select:
      enabled: true
      ingressClassName: nginx
      annotations:
        nginx.ingress.kubernetes.io/ssl-redirect: "false"
      hosts:
        - vmselect.apatsev.org.ru
```

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
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

### Генерация нагрузки (без сборки образа)

На момент написания README образ приложения в `ghcr.io/patsevanton/strimzi-kafka-chaos-testing` использует `segmentio/kafka-go` и может требовать обновления/пересборки для полной совместимости с Kafka `4.x`. Чтобы **гарантированно** получить нагрузку без сборки образов, используйте Kafka CLI из образа Strimzi.

Если вы не хотите собирать/публиковать Docker-образ приложения, можно генерировать нагрузку штатными утилитами Kafka из образа Strimzi.

```bash
kubectl create namespace kafka-app --dry-run=client -o yaml | kubectl apply -f -

# Под-утилита с Kafka CLI
kubectl run kafka-client -n kafka-app \
  --image=quay.io/strimzi/kafka:0.50.0-kafka-4.1.1 \
  --restart=Never --command -- sleep 3600
kubectl wait pod/kafka-client -n kafka-app --for=condition=Ready --timeout=120s

# Produce нагрузка (пример)
PASS=$(kubectl get secret myuser -n kafka-cluster -o jsonpath='{.data.password}' | base64 -d)
kubectl exec -n kafka-app kafka-client -- bash -lc "\
cat > /tmp/client.properties <<'EOF'
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-512
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username=\"myuser\" password=\"$PASS\";
EOF
/opt/kafka/bin/kafka-producer-perf-test.sh \
  --topic test-topic \
  --num-records 100000 \
  --record-size 256 \
  --throughput -1 \
  --producer-props bootstrap.servers=kafka-cluster-kafka-bootstrap.kafka-cluster:9092 \
  --producer.config /tmp/client.properties"
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

## Удаление компонентов

### Удаление всех компонентов и namespace

```bash
# Удаление Kafka Application
kubectl delete pod kafka-client -n kafka-app
kubectl delete -f schema-registry.yaml
kubectl delete secret schema-registry-credentials -n kafka-app

# Удаление Kafka кластера
kubectl delete kafkatopic -n kafka-cluster --all
kubectl delete kafkauser -n kafka-cluster --all
kubectl delete kafka kafka-cluster -n kafka-cluster

# Удаление Strimzi
helm uninstall strimzi -n strimzi

# Удаление Chaos Mesh
helm uninstall chaos-mesh -n chaos-mesh

# Удаление VictoriaLogs
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
helm uninstall victoria-logs-collector -n victoria-logs-collector

# Удаление VictoriaMetrics K8s Stack
helm uninstall vmks -n vmks

# Удаление всех namespace
kubectl delete namespace kafka-app
kubectl delete namespace kafka-cluster
kubectl delete namespace strimzi
kubectl delete namespace chaos-mesh
kubectl delete namespace victoria-logs-cluster
kubectl delete namespace victoria-logs-collector
kubectl delete namespace vmks
```
