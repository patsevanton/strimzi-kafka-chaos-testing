# Тестирование Strimzi Kafka под высокой нагрузкой с помощью Chaos Mesh

Данный проект демонстрирует подход к тестированию отказоустойчивости и производительности кластера Apache Kafka, развернутого через оператор Strimzi в Kubernetes, под воздействием различных типов сбоев, создаваемых платформой Chaos Mesh.

Для обеспечения наблюдаемости во время тестирования используется observability stack на базе VictoriaLogs и VictoriaMetrics, который позволяет отслеживать состояние системы, анализировать логи и метрики в реальном времени.

## Strimzi

**Strimzi** — оператор Kubernetes для развертывания и управления Apache Kafka в Kubernetes. Предоставляет Custom Resource Definitions (CRDs) для управления Kafka-кластерами, топиками, пользователями и подключениями.

### Установка Strimzi

```bash
helm repo add strimzi https://strimzi.io/charts/
helm repo update
helm install strimzi strimzi/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --wait
```

Проверка установки:

```bash
kubectl get pods -n strimzi
```

## Chaos Mesh

**Chaos Mesh** — облачная платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

### Установка Chaos Mesh

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update
helm install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh \
  --create-namespace \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock \
  --wait
```

Проверка установки:

```bash
kubectl get pods -n chaos-mesh
```

### Доступ к Dashboard

```bash
kubectl port-forward -n chaos-mesh svc/chaos-dashboard 2333:2333
```

Откройте в браузере: `http://localhost:2333`

## Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из всех компонентов кластера Kafka и приложений, работающих с ним.

### VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

#### Установка: Single Node (рекомендуется для начала)

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

Single node версия запускает один под, который объединяет все функции (ingest, storage, query). Это упрощает установку и требует меньше ресурсов. При необходимости можно перейти на кластерную версию без миграции данных.

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
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
    tls:
      - hosts:
        - victorialogs.apatsev.org.ru
        secretName: victorialogs-tls
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
    # Поле 'http.uri' для HTTP-запросов, если логи содержат URI как сообщение.
    - http.uri
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
      cert-manager.io/cluster-issuer: letsencrypt-prod
    tls:
      - hosts:
          - grafana.apatsev.org.ru
        secretName: grafana-tls
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
        cert-manager.io/cluster-issuer: letsencrypt-prod
      hosts:
        - vmselect.apatsev.org.ru
      tls:
        - secretName: victoriametrics-tls
          hosts:
            - vmselect.apatsev.org.ru
```

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```

## Kafka Application

**Kafka Application** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Используется для генерации нагрузки на кластер Kafka во время тестирования.

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

### Установка и запуск приложения

Приложение развертывается в Kubernetes через Helm-чарты. Доступны два отдельных чарта: `kafka-producer` и `kafka-consumer`.

#### Producer режим

```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-app \
  --create-namespace \
  --set kafka.brokers="kafka-broker-1:9092,kafka-broker-2:9092" \
  --set kafka.topic="test-topic" \
  --set schemaRegistry.url="http://schema-registry:8081" \
  --set kafka.username="myuser" \
  --set kafka.password="mypassword"
```

Или используйте файл values:

```bash
# Создайте файл producer-values.yaml
cat > producer-values.yaml <<EOF
kafka:
  brokers: "kafka-broker-1:9092,kafka-broker-2:9092"
  topic: "test-topic"
  username: "myuser"
  password: "mypassword"
schemaRegistry:
  url: "http://schema-registry:8081"
EOF

helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-app \
  --create-namespace \
  -f producer-values.yaml
```

#### Consumer режим

```bash
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-app \
  --create-namespace \
  --set kafka.brokers="kafka-broker-1:9092,kafka-broker-2:9092" \
  --set kafka.topic="test-topic" \
  --set kafka.groupId="my-consumer-group" \
  --set schemaRegistry.url="http://schema-registry:8081" \
  --set kafka.username="myuser" \
  --set kafka.password="mypassword"
```

Или используйте файл values:

```bash
# Создайте файл consumer-values.yaml
cat > consumer-values.yaml <<EOF
kafka:
  brokers: "kafka-broker-1:9092,kafka-broker-2:9092"
  topic: "test-topic"
  groupId: "my-consumer-group"
  username: "myuser"
  password: "mypassword"
schemaRegistry:
  url: "http://schema-registry:8081"
EOF

helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-app \
  --create-namespace \
  -f consumer-values.yaml
```

#### Использование Secrets для учетных данных

Для более безопасного хранения учетных данных можно использовать Kubernetes Secrets:

```bash
# Producer с secrets
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-app \
  --create-namespace \
  --set secrets.create=true \
  --set secrets.username="myuser" \
  --set secrets.password="mypassword" \
  --set kafka.brokers="kafka-broker-1:9092,kafka-broker-2:9092" \
  --set kafka.topic="test-topic" \
  --set schemaRegistry.url="http://schema-registry:8081"

# Consumer с secrets
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-app \
  --create-namespace \
  --set secrets.create=true \
  --set secrets.username="myuser" \
  --set secrets.password="mypassword" \
  --set kafka.brokers="kafka-broker-1:9092,kafka-broker-2:9092" \
  --set kafka.topic="test-topic" \
  --set kafka.groupId="my-consumer-group" \
  --set schemaRegistry.url="http://schema-registry:8081"
```

#### Проверка статуса

```bash
# Проверка подов
kubectl get pods -n kafka-app

# Просмотр логов producer
kubectl logs -n kafka-app -l app.kubernetes.io/name=kafka-producer

# Просмотр логов consumer
kubectl logs -n kafka-app -l app.kubernetes.io/name=kafka-consumer
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

### Удаление Strimzi

```bash
helm uninstall strimzi -n strimzi
```

### Удаление Chaos Mesh

```bash
helm uninstall chaos-mesh -n chaos-mesh
```

### Удаление VictoriaLogs

```bash
helm uninstall -n victoria-logs-cluster victoria-logs-cluster
```

### Удаление Kafka Application

```bash
helm uninstall kafka-producer -n kafka-app
helm uninstall kafka-consumer -n kafka-app
```
