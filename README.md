# Тестирование Strimzi Kafka под высокой нагрузкой

Проект для тестирования **отказоустойчивости**, **производительности**, **хаос-тестов**, **мониторинга**, **Schema Registry**, **Kafka UI** и **golang app** (producer/consumer) высоконагруженного кластера Apache Strimzi Kafka в Kubernetes. Пошагово разворачивается мониторинг на базе Helm-чарта **VictoriaMetrics K8s Stack**: установка стека и Grafana, Strimzi Operator и Kafka-кластера с JMX и Kafka Exporter, настройка сбора метрик через VMPodScrape/VMServiceScrape и отдельного kube-state-metrics для Strimzi CRD, Schema Registry (Karapace) для Avro, а также Go producer/consumer с готовыми Helm-чартами.

## Установка стека мониторинга (VictoriaMetrics K8s Stack)

1. Репозиторий Helm для VictoriaMetrics (нужен для VictoriaLogs и других чартов ниже; сам VictoriaMetrics K8s Stack ставится из OCI):

```bash
helm repo add vm https://victoriametrics.github.io/helm-charts/
helm repo update
```

2. Установить VictoriaMetrics K8s Stack с values из `victoriametrics-values.yaml` (Ingress для Grafana на `grafana.apatsev.org.ru`). Имя релиза и namespace `vmks` выбраны короткими, чтобы не упираться в лимит 63 символа для имён ресурсов Kubernetes. При ошибке загрузки чарта (например, EOF) повторите команду:

```bash
helm upgrade --install vmks \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-metrics-k8s-stack \
  --namespace vmks \
  --create-namespace \
  --wait \
  --version 0.68.0 \
  --timeout 15m \
  -f victoriametrics-values.yaml
```

3. Получить пароль администратора Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```

4. Открыть Grafana: http://grafana.apatsev.org.ru (логин по умолчанию: `admin`). Datasource VictoriaMetrics добавляется автоматически.

### Strimzi

Strimzi — оператор для управления Kafka в Kubernetes; мониторинг вынесен в отдельные компоненты (Kafka Exporter, kube-state-metrics, PodMonitors для брокеров и операторов).

### Установка Strimzi

Namespace `kafka-cluster` должен существовать заранее (как в оригинале strimzi-kafka-chaos-testing):

```bash
# Идемпотентно: создаёт namespace только если его ещё нет
kubectl get ns kafka-cluster 2>/dev/null || kubectl create namespace kafka-cluster
```

```bash
helm upgrade --install strimzi-cluster-operator \
  oci://quay.io/strimzi-helm/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --set 'watchNamespaces={kafka-cluster}' \
  --wait \
  --version 0.50.0
```

> **Чем отличаются манифесты от upstream Strimzi:** все VMPodScrape и VMServiceScrape заранее помечены `release: vmks` (VictoriaMetrics K8s Stack 0.70+ не устанавливает Prometheus Operator CRD PodMonitor/ServiceMonitor, используются CRD VictoriaMetrics Operator). Манифест `cluster-operator-metrics` сразу смотрит в namespace `strimzi`, а Service для `strimzi-kube-state-metrics` уже содержит необходимые `app.kubernetes.io/*` метки. Если использовать оригинальные yaml из [официального репозитория Strimzi](https://github.com/strimzi/strimzi-kafka-operator/tree/main/packaging/examples/metrics), добавьте эти label вручную (`release: vmks` на VMPodScrape/VMServiceScrape и `app.kubernetes.io/*` на Service) и поправьте `namespaceSelector.matchNames` для `cluster-operator-metrics` на `strimzi`.

Манифесты из [examples](https://github.com/strimzi/strimzi-kafka-operator/tree/main/examples) Strimzi сохранены локально в директории **strimzi/** (kafka-metrics, kafka-topic, kafka-user, VMPodScrape/VMServiceScrape, kube-state-metrics). Вы можете использовать оригинальные манифесты + добавление label или можете использовать манифесты из текущего репозитория.

### Установка Kafka из examples

Kafka разворачивается с **внутренним listener на порту 9092 с аутентификацией SASL SCRAM-SHA-512** и **авторизацией simple** (для ACL в KafkaUser). Клиенты (Producer, Consumer, Schema Registry) подключаются с учётными данными KafkaUser. В **kafka-metrics.yaml** уже заданы `authorization.type: simple`; без этого KafkaUser с ACL не перейдёт в Ready и Secret `myuser` не будет создан.

```bash
# Kafka-кластер (KRaft, persistent, listener sasl:9092 с SCRAM-SHA-512, JMX и Kafka Exporter)
kubectl apply -n kafka-cluster -f strimzi/kafka-metrics.yaml

# Топик
kubectl apply -n kafka-cluster -f strimzi/kafka-topic.yaml

# Пользователь Kafka (SCRAM-SHA-512; оператор создаёт Secret myuser с паролем)
kubectl apply -n kafka-cluster -f strimzi/kafka-user.yaml
```

```bash
# Дождаться готовности Kafka (при первом развёртывании может занять 10–15 минут)
kubectl wait kafka/kafka-cluster -n kafka-cluster --for=condition=Ready --timeout=900s
```

### PodDisruptionBudget для Kafka

PodDisruptionBudget гарантирует, что минимум 2 брокера всегда доступны во время плановых прерываний (drain ноды, rolling updates).

```bash
kubectl apply -n kafka-cluster -f strimzi/kafka-pdb.yaml
kubectl get pdb -n kafka-cluster
```

### Metrics (examples/metrics)

Кластер Kafka задаётся манифестом **kafka-metrics.yaml** (ресурс `Kafka` CR Strimzi) — JMX-метрики (`metricsConfig`) и Kafka Exporter уже включены в манифест. Остаётся применить VMPodScrape для сбора метрик в VMAgent.

```bash
# Сбор метрик Strimzi Cluster Operator (состояние оператора, реконсиляция)
kubectl apply -n vmks -f strimzi/cluster-operator-metrics.yaml

# Сбор метрик Entity Operator — Topic Operator и User Operator
kubectl apply -n vmks -f strimzi/entity-operator-metrics.yaml

# Сбор JMX-метрик с подов брокеров Kafka
kubectl apply -n vmks -f strimzi/kafka-resources-metrics.yaml
```

**Kube-state-metrics для Strimzi CRD** — отдельный экземпляр [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) в режиме `--custom-resource-state-only`: он следит за **кастомными ресурсами Strimzi** (Kafka, KafkaTopic, KafkaUser, KafkaConnect, KafkaConnector и др.) и отдаёт их состояние в формате Prometheus (ready, replicas, topicId, kafka_version и т.д.). Это нужно для дашбордов и алертов по состоянию CR (например, «топик не Ready», «Kafka не на целевой версии»). Обычный kube-state-metrics из VictoriaMetrics K8s Stack таких метрик по Strimzi не даёт.

- **Шаг 1 (ConfigMap):** описание, какие CRD и какие поля из них экспортировать как метрики (префиксы `strimzi_kafka_topic_*`, `strimzi_kafka_user_*`, `strimzi_kafka_*` и т.д.).
- **Шаг 2 (Deployment + RBAC + VMServiceScrape):** сам под kube-state-metrics с этим конфигом, права на list/watch Strimzi CR в кластере и VMServiceScrape, чтобы VMAgent начал скрейпить метрики.

```bash
# 1. ConfigMap с конфигом метрик по CRD Strimzi
kubectl apply -n kafka-cluster -f strimzi/kube-state-metrics-configmap.yaml

# 2. Deployment, Service, RBAC и VMServiceScrape
kubectl apply -n kafka-cluster -f strimzi/kube-state-metrics-ksm.yaml
```

## Kafka Exporter

Kafka Exporter подключается к брокерам по Kafka API и отдаёт метрики в формате Prometheus.

**kafka-metrics.yaml** уже включает блок **`spec.kafkaExporter`** в ресурсе `Kafka` (CR Strimzi). Этот блок включает Kafka Exporter: без него оператор не создаёт соответствующие ресурсы, а при его наличии — автоматически разворачивает Deployment, Pod и Service в namespace кластера.

**VMServiceScrape для Strimzi Kafka Exporter:** Strimzi создаёт Service `kafka-cluster-kafka-exporter` в kafka-cluster. Создайте VMServiceScrape, чтобы VMAgent собирал метрики топиков и consumer groups:

```bash
kubectl apply -f strimzi/kafka-exporter-servicemonitor.yaml
```

При указании `kafkaExporter` в CR Strimzi Cluster Operator поднимает **отдельный Deployment** (например, `kafka-cluster-kafka-exporter`) — это не «просто параметр» в поде Kafka, а отдельное приложение, которым управляет оператор.

Kafka Exporter **встроен в Strimzi** как опциональный компонент: образ и конфигурация задаются оператором, он создаёт и обновляет Deployment/Service при изменении CR.

### Schema Registry (Karapace) для Avro

Go-приложение из этого репозитория использует Avro и Schema Registry API. Для удобства здесь добавлены готовые манифесты для **[Karapace](https://github.com/Aiven-Open/karapace)** — open-source реализации API Confluent Schema Registry (drop-in replacement).

Karapace поднимается как обычный HTTP-сервис и хранит схемы в Kafka-топике `_schemas` (как и Confluent SR).

- `strimzi/kafka-topic-schemas.yaml` — KafkaTopic для `_schemas` (важно при `min.insync.replicas: 2`)
- `strimzi/kafka-user-schema-registry.yaml` — отдельный KafkaUser для Karapace с минимальными правами (топик `_schemas`, consumer groups)
- `schema-registry.yaml` — Service/Deployment для Karapace (`ghcr.io/aiven-open/karapace:5.0.3`). Подключение к Kafka по **SASL SCRAM-SHA-512** (логин/пароль из KafkaUser `schema-registry`). Развёрнуто **2 реплики** для отказоустойчивости (PDB, rolling update без простоя). У всех реплик `KARAPACE_MASTER_ELIGIBILITY=true` (выбор master через Kafka consumer group).

Файлы `strimzi/` в репозитории используют `namespace: kafka-cluster` и `strimzi.io/cluster: kafka-cluster`. В `schema-registry.yaml` задан `KARAPACE_BOOTSTRAP_URI`: `kafka-cluster-kafka-bootstrap.kafka-cluster.svc.cluster.local:9092`. Подставьте свой namespace/кластер, если иные.

**Секрет для Schema Registry:** Karapace читает пароль из Secret `schema-registry` в namespace `schema-registry`. Strimzi создаёт этот Secret в `kafka-cluster` после применения `kafka-user-schema-registry.yaml`. Скопируйте Secret в namespace `schema-registry` перед развёртыванием Karapace:

```bash
kubectl create namespace schema-registry --dry-run=client -o yaml | kubectl apply -f -

# Создать KafkaUser для Schema Registry
kubectl apply -n kafka-cluster -f strimzi/kafka-user-schema-registry.yaml
kubectl wait kafkauser/schema-registry -n kafka-cluster --for=condition=Ready --timeout=60s || true
# Если таймаут: проверьте kubectl get kafkauser schema-registry -n kafka-cluster; при Ready продолжайте.

# Скопировать Secret schema-registry в namespace schema-registry.
# Важно: убрать ownerReferences, иначе в новом namespace Secret будет невалидным (используйте jq).
kubectl get secret schema-registry -n kafka-cluster -o json | \
  jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.ownerReferences) | .metadata.namespace = "schema-registry"' | \
  kubectl apply -f -

# Создать топик для схем
kubectl apply -n kafka-cluster -f strimzi/kafka-topic-schemas.yaml
kubectl wait kafkatopic/schemas-topic -n kafka-cluster --for=condition=Ready --timeout=120s || true
# Если таймаут: проверьте kubectl get kafkatopic schemas-topic -n kafka-cluster; при Ready продолжайте.

# Развернуть Schema Registry
kubectl apply -f schema-registry.yaml
kubectl rollout status deploy/schema-registry -n schema-registry --timeout=5m || true
# При таймауте (загрузка образа, выбор master): проверьте kubectl get pods -n schema-registry; дождитесь Ready, затем sleep 120.
sleep 120
kubectl get svc -n schema-registry schema-registry
```

**Ожидание:** `sleep 120` или дольше нужен после первого запуска Karapace, чтобы успел выбраться master; иначе приложение Producer при регистрации схем может получить ошибку 503. Команды `kubectl wait` для KafkaUser/KafkaTopic и `kubectl rollout status` в некоторых окружениях могут завершаться по таймауту при уже готовых ресурсах — тогда проверьте статус вручную и продолжайте.

> **Важно: ACL для Karapace.** KafkaUser `schema-registry` содержит ACL для топика `_schemas`, consumer group `schema-registry` и групп с префиксом `karapace` (karapace-autogenerated-*). Без этих прав Schema Registry «зависнет» на `Replay progress: -1/N`.

## Producer App и Consumer App

**Producer App и Consumer App** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Сообщения сериализуются в **Avro** с использованием **Schema Registry (Karapace)** — совместимого с Confluent API. Kafka использует **аутентификацию SASL SCRAM-SHA-512**; учётные данные передаются **только через Secret** (kind: Secret, например `myuser` от Strimzi). Перед запуском Producer/Consumer необходимо развернуть Schema Registry (см. раздел «Schema Registry (Karapace) для Avro») и передать `schemaRegistry.url` и учётные данные Kafka в Helm.

### Используемые библиотеки

- **[segmentio/kafka-go](https://github.com/segmentio/kafka-go)** — клиент для работы с Kafka
- **[riferrei/srclient](https://github.com/riferrei/srclient)** — клиент для Schema Registry API (совместим с Karapace)
- **[linkedin/goavro](https://github.com/linkedin/goavro)** — работа с Avro схемами
- **[prometheus/client_golang](https://github.com/prometheus/client_golang)** — экспорт Prometheus-метрик

### Структура исходного кода

- `main.go` — основной код Go-приложения (producer/consumer)
- `metrics.go` — определение Prometheus-метрик
- `go.mod`, `go.sum` — файлы зависимостей Go модуля
- `Dockerfile` — многоэтапная сборка Docker образа

### Сборка и публикация Docker образа

Go-код в `[main.go](https://github.com/patsevanton/strimzi-kafka-chaos-testing/blob/main/main.go)` можно изменять под свои нужды. После внесения изменений соберите и опубликуйте Docker образ:

```bash
# Сборка образа (используйте podman или docker)
podman build -t docker.io/antonpatsev/strimzi-kafka-chaos-testing:0.2.5 .

# Публикация в Docker Hub
podman push docker.io/antonpatsev/strimzi-kafka-chaos-testing:0.2.5
```

После публикации обновите версию образа в Helm values или передайте через `--set`:

```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set image.repository="docker.io/antonpatsev/strimzi-kafka-chaos-testing" \
  --set image.tag="0.2.5"
```

### Переменные окружения

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `MODE` | Режим работы: `producer` или `consumer` | `producer` |
| `KAFKA_BROKERS` | Список брокеров Kafka (через запятую) | `localhost:9092` |
| `KAFKA_TOPIC` | Название топика | `test-topic` (как в [Strimzi examples](https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/topic/kafka-topic.yaml)) |
| `KAFKA_USERNAME` | Имя пользователя Kafka (SASL SCRAM-SHA-512), обязательно | — |
| `KAFKA_PASSWORD` | Пароль пользователя Kafka (из Secret `myuser` в Strimzi), обязательно | — |
| `SCHEMA_REGISTRY_URL` | URL Schema Registry | `http://localhost:8081` |
| `KAFKA_GROUP_ID` | Consumer Group ID (только для consumer) | `test-group` (как в [Strimzi kafka-user](https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/user/kafka-user.yaml)) |
| `HEALTH_PORT` | Порт для health-проверок (liveness/readiness) | `8080` |
| `REDIS_ADDR` | Адрес Redis для верификации доставки (хеш тела сообщения) | `localhost:6379` |
| `REDIS_PASSWORD` | Пароль Redis (если нужен) | — |
| `REDIS_KEY_PREFIX` | Префикс ключей сообщений в Redis | `kafka-msg:` |
| `REDIS_SLO_SECONDS` | Порог в секундах: сообщения в Redis старше этого считаются нарушением SLO | `60` |

**Верификация доставки через Redis:** при указании `REDIS_ADDR` Producer после отправки в Kafka записывает в Redis ключ (как у сообщения) и значение = SHA256 тела + timestamp. Consumer при получении сверяет хеш тела с Redis, при совпадении удаляет ключ и увеличивает счётчик полученных. Метрики `redis_pending_messages` и `redis_pending_old_messages` (старее `REDIS_SLO_SECONDS`) дают SLO по задержке доставки. Критика подхода — в [docs/delivery-verification-critique.md](docs/delivery-verification-critique.md).

### Запуск Producer/Consumer в кластере используя Helm

Для запуска приложений в кластере используйте Helm charts из директории `helm`. Kafka использует **SASL SCRAM-SHA-512**; учётные данные KafkaUser передаются **только через Secret** (kind: Secret) — указывается `kafka.existingSecret="myuser"` (Secret создаётся Strimzi при применении `kafka-user.yaml`). Имена приведены к [примерам Strimzi](https://github.com/strimzi/strimzi-kafka-operator/tree/main/packaging/examples): `test-topic`, `test-group`, пользователь `myuser`.

**Секрет в namespace Producer/Consumer:** Учётные данные Kafka (Secret `myuser` от Strimzi) должны быть в namespace `kafka-producer` и `kafka-consumer`. Скопируйте Secret из `kafka-cluster` один раз после применения `kafka-user.yaml` и готовности Kafka:

```bash
# 1. Убедиться, что Secret myuser есть в kafka-cluster (создаётся Strimzi User Operator после kafka-user.yaml)
kubectl get secret myuser -n kafka-cluster

# 2. Создать namespace для Producer и Consumer (если ещё нет)
kubectl create namespace kafka-producer --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace kafka-consumer --dry-run=client -o yaml | kubectl apply -f -

# 3. Скопировать Secret myuser с кредами в kafka-producer и kafka-consumer (через jq, без ownerReferences)
kubectl get secret myuser -n kafka-cluster -o json | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.ownerReferences) | .metadata.namespace = "kafka-producer"' | kubectl apply -f -
kubectl get secret myuser -n kafka-cluster -o json | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.ownerReferences) | .metadata.namespace = "kafka-consumer"' | kubectl apply -f -

# 4. Проверить: Secret должен быть в обоих namespace
# (при необходимости подождать 2–3 с и повторить)
kubectl get secret myuser -n kafka-producer
kubectl get secret myuser -n kafka-consumer
```

#### 1) Установить Producer
```bash
# Адрес Valkey (после terraform apply): VALKEY_ADDR=$(terraform output -raw valkey_address)
# С Valkey: добавьте --set redis.addr="$VALKEY_ADDR" --set redis.password="strimzi-valkey-test"
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set image.tag="0.2.5" \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster.svc.cluster.local:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry:8081" \
  --set kafka.topic="test-topic" \
  --set kafka.existingSecret="myuser"
```

#### 2) Установить Consumer
```bash
# С Valkey: --set redis.addr="$VALKEY_ADDR" --set redis.password="strimzi-valkey-test"
helm upgrade --install kafka-consumer ./helm/kafka-consumer \
  --namespace kafka-consumer \
  --create-namespace \
  --set image.tag="0.2.5" \
  --set kafka.brokers="kafka-cluster-kafka-bootstrap.kafka-cluster.svc.cluster.local:9092" \
  --set schemaRegistry.url="http://schema-registry.schema-registry:8081" \
  --set kafka.topic="test-topic" \
  --set kafka.groupId="test-group" \
  --set kafka.existingSecret="myuser"
```

**Valkey (Redis) для верификации доставки:** кластер задаётся в `valkey.tf` (Yandex Managed Redis). Настройте провайдер Yandex (`token` или `service_account_key_file`), выполните `terraform apply -auto-approve`, затем `export VALKEY_ADDR=$(terraform output -raw valkey_address)` и добавьте к командам Helm выше: `--set redis.addr="$VALKEY_ADDR" --set redis.password="strimzi-valkey-test"`. Либо укажите `redis.addr` и `redis.password` в `values.yaml` (уже заданы для текущего кластера). Чтобы в Grafana отображались метрики Redis и дашборд **redis-delivery-verification**, выполните шаги из подраздела [Redis Exporter и дашборд redis-delivery-verification](#redis-exporter-и-дашборд-redis-delivery-verification) ниже.

Пример вывода Terraform:
```bash
$ terraform output external_valkey
{
  "host"     = "c-c9qqfhgq1u91e1mq3l5u.rw.mdb.yandexcloud.net"
  "password" = "strimzi-valkey-test"
  "port"     = 6379
}

$ terraform output -raw valkey_address
c-c9qqfhgq1u91e1mq3l5u.rw.mdb.yandexcloud.net:6379
```

### Redis Exporter и дашборд redis-delivery-verification

Чтобы в дашборде **redis-delivery-verification** (Grafana) отображались метрики Redis/Valkey (latency, нагрузка, SLO верификации доставки), нужно развернуть redis-exporter и настроить сбор метрик. Адрес и пароль Redis возьмите из Terraform (`terraform output external_valkey` / `valkey_address`) или укажите свой инстанс.

1. **Создать Secret с учётными данными Redis** в namespace `vmks` (имя Secret должно быть `valkey-exporter-redis` — его ожидает манифест redis-exporter):

```bash
# Подставьте свой адрес и пароль (например из terraform output -raw valkey_address)
kubectl create secret generic valkey-exporter-redis -n vmks \
  --from-literal=REDIS_ADDR='<host>:6379' \
  --from-literal=REDIS_PASSWORD='<password>'
```

2. **Применить манифест redis-exporter** (Deployment, Service и VMServiceScrape в одном файле):

```bash
kubectl apply -f strimzi/redis-exporter.yaml
```

3. **Проверить, что под redis-exporter запущен** и VMAgent собирает метрики:

```bash
kubectl get pods -n vmks -l app.kubernetes.io/name=redis-exporter
kubectl get vmservicescrape -n vmks redis-exporter
```

4. **Импортировать дашборд в Grafana** (если ещё не импортирован): Dashboards → Import → загрузить `dashboards/redis-delivery-verification.json`. В качестве источника метрик выберите VictoriaMetrics. После появления данных с redis-exporter панели дашборда начнут отображать метрики Redis и (при включённой верификации доставки) SLO.

Подробнее о верификации доставки и метриках — [docs/delivery-verification-critique.md](docs/delivery-verification-critique.md).

#### 3) Дождаться готовности подов Producer/Consumer
```bash
kubectl rollout status deploy/kafka-producer -n kafka-producer --timeout=120s
kubectl rollout status deploy/kafka-consumer -n kafka-consumer --timeout=120s
# Либо следить за подами: kubectl get pods -n kafka-producer; kubectl get pods -n kafka-consumer -w
```

#### 4) Проверка подов и логов
```bash
# Убедиться, что все поды в статусе Running
kubectl get pods -n kafka-producer
kubectl get pods -n kafka-consumer
kubectl get pods -n schema-registry

# Producer logs (проверка на ошибки)
kubectl logs -n kafka-producer -l app.kubernetes.io/name=kafka-producer -f

# Consumer logs (проверка на ошибки)
kubectl logs -n kafka-consumer -l app.kubernetes.io/name=kafka-consumer -f
```

#### 5) Настроить сбор метрик Prometheus

Go-приложение экспортирует метрики Prometheus на endpoint `/metrics` (на том же порту, что и health checks). Для сбора метрик через VMAgent примените VMServiceScrape:

```bash
# Метрики Producer
kubectl apply -f strimzi/kafka-producer-metrics.yaml

# Метрики Consumer
kubectl apply -f strimzi/kafka-consumer-metrics.yaml
```

**Доступные метрики:**

**Producer метрики:**
- `kafka_producer_messages_sent_total{topic}` — общее количество отправленных сообщений
- `kafka_producer_messages_sent_bytes_total{topic}` — общий объём отправленных данных (байты)
- `kafka_producer_message_send_duration_seconds{topic}` — время отправки сообщения (от создания до подтверждения Kafka)
- `kafka_producer_message_encode_duration_seconds{topic}` — время кодирования сообщения в Avro
- `kafka_producer_errors_total{topic,error_type}` — количество ошибок (error_type: encode, send, connection)

**Consumer метрики:**
- `kafka_consumer_messages_received_total{topic,partition}` — общее количество полученных сообщений
- `kafka_consumer_messages_received_bytes_total{topic,partition}` — общий объём полученных данных (байты)
- `kafka_consumer_message_processing_duration_seconds{topic,partition}` — время обработки сообщения (от получения до завершения)
- `kafka_consumer_message_decode_duration_seconds{topic,partition}` — время декодирования сообщения из Avro
- `kafka_consumer_end_to_end_latency_seconds{topic,partition}` — end-to-end задержка от создания сообщения (timestamp) до потребления
- `kafka_consumer_errors_total{topic,error_type}` — количество ошибок (error_type: read, decode, connection)
- `kafka_consumer_lag{topic,partition,group_id}` — отставание consumer (разница между последним offset и offset consumer)

**Schema Registry метрики:**
- `schema_registry_requests_total{operation}` — количество запросов к Schema Registry (operation: get_schema, get_latest_schema, create_schema)
- `schema_registry_request_duration_seconds{operation}` — длительность запросов к Schema Registry
- `schema_registry_errors_total{operation,error_type}` — количество ошибок (error_type: timeout, not_found, invalid_schema, network)

**Метрики подключения:**
- `kafka_connection_status{broker}` — статус подключения к Kafka (1 = подключено, 0 = отключено)
- `kafka_reconnections_total{broker}` — количество переподключений к Kafka
- `schema_registry_connection_status` — статус подключения к Schema Registry (1 = подключено, 0 = отключено)

**Отличия от метрик Kafka:**

Метрики Kafka (через JMX и Kafka Exporter) показывают состояние на стороне брокера:
- Количество сообщений, полученных брокером
- Latency обработки на брокере
- Размер топиков, количество партиций
- Lag consumer groups на уровне брокера

Метрики Go-приложения дополняют их данными на уровне приложения:
- **Application-level latency** — время от создания сообщения до отправки/получения (включая кодирование/декодирование)
- **End-to-end latency** — полная задержка от создания до потребления (на основе timestamp в сообщении)
- **Schema Registry метрики** — производительность работы со схемами, кэширование
- **Ошибки приложения** — ошибки кодирования/декодирования, проблемы подключения на стороне клиента
- **Размер сообщений** — объём данных, отправляемых/получаемых приложением

Эти метрики помогают диагностировать проблемы производительности на стороне клиента, которые не видны в метриках брокера.

### Kafka UI

Web-интерфейс для управления Kafka — просмотр топиков, consumer groups, сообщений. Используется чарт [kafbat-ui/kafka-ui](https://github.com/kafbat/helm-charts).

```bash
# Добавить Helm-репозиторий
helm repo add kafbat-ui https://kafbat.github.io/helm-charts
helm repo update

# Kafka UI использует отдельный read-only пользователь kafka-ui-user
kubectl apply -n kafka-cluster -f strimzi/kafka-user-kafka-ui.yaml
kubectl wait kafkauser/kafka-ui-user -n kafka-cluster --for=condition=Ready --timeout=60s || true
# При таймауте: kubectl get kafkauser kafka-ui-user -n kafka-cluster; при Ready продолжайте.

kubectl create namespace kafka-ui --dry-run=client -o yaml | kubectl apply -f -
kubectl get secret kafka-ui-user -n kafka-cluster -o json | \
  jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.ownerReferences) | .metadata.namespace = "kafka-ui"' | \
  kubectl apply -f -

# Установить Kafka UI
helm upgrade --install kafka-ui kafbat-ui/kafka-ui \
  -f helm/kafka-ui-values.yaml \
  --namespace kafka-ui \
  --create-namespace

# Дождаться готовности (при первом запуске Kafka UI может потребоваться 2–3 минуты)
kubectl rollout status deploy/kafka-ui -n kafka-ui --timeout=300s
# При таймауте: kubectl get pods -n kafka-ui; при Running/Ready продолжайте.
```

Kafka UI будет доступен по адресу Ingress (в values: `kafka-ui.apatsev.org.ru`). Значения в `helm/kafka-ui-values.yaml` адаптированы под кластер `kafka-cluster` в namespace `kafka-cluster` и Schema Registry в `schema-registry`. Kafka UI подключается под пользователем **kafka-ui-user** с правами только на чтение (Describe, Read на topics и groups).

## VictoriaLogs

**VictoriaLogs** — хранилище логов от VictoriaMetrics с поддержкой LogsQL в Grafana.

**Важно:** VictoriaMetrics K8s Stack должен быть установлен первым (он предоставляет CRD VMServiceScrape и т.п., используемые чартом VictoriaLogs).

### Установка VictoriaLogs (cluster)

Используется файл `victoria-logs-cluster-values.yaml` из репозитория (Ingress для vlselect на `victorialogs.apatsev.org.ru`, retention 1d, PVC 20Gi).

```bash
# Репозиторий vm уже добавлен при установке VictoriaMetrics K8s Stack; при необходимости:
helm repo add vm https://victoriametrics.github.io/helm-charts/
helm repo update

helm upgrade --install victoria-logs-cluster vm/victoria-logs-cluster \
  --namespace victoria-logs-cluster \
  --create-namespace \
  --wait \
  --version 0.0.27 \
  --timeout 15m \
  -f victoria-logs-cluster-values.yaml \
  --set vlselect.vmServiceScrape.enabled=true \
  --set vlinsert.vmServiceScrape.enabled=true \
  --set vlstorage.vmServiceScrape.enabled=true
```

Чтобы VMAgent из VictoriaMetrics K8s Stack собирал метрики VictoriaLogs, на VMServiceScrape должен быть label, по которому стэк выбирает цели (например `release: vmks`). Если чарт по умолчанию задаёт другой `release`, добавьте в values или `--set` нужный label для vlselect/vlinsert/vlstorage VMServiceScrape.

Проверка: `kubectl get pods -n victoria-logs-cluster`. Доступ к UI: по адресу Ingress из values (по умолчанию `victorialogs.apatsev.org.ru`).

### Victoria-logs-collector (опционально)

**Victoria-logs-collector** — Helm-чарт VictoriaMetrics, разворачивающий агент сбора логов (vlagent) как DaemonSet. Собирает логи со всех контейнеров в кластере и отправляет их в VictoriaLogs (vlinsert).

**Требование:** перед установкой должен быть развёрнут VictoriaLogs cluster (см. выше).

Используется файл `victoria-logs-collector-values.yaml` из репозитория (адрес vlinsert, поля для игнорирования, поля сообщения лога).

```bash
helm upgrade --install victoria-logs-collector vm/victoria-logs-collector \
  --namespace victoria-logs-collector \
  --create-namespace \
  --wait \
  --version 0.2.8 \
  --timeout 15m \
  -f victoria-logs-collector-values.yaml \
  --set podMonitor.enabled=true \
  --set podMonitor.vm=true
```

Параметр `podMonitor.vm=true` создаёт VMPodScrape для сбора метрик коллектора в VictoriaMetrics K8s Stack.

Проверка: `kubectl get pods -n victoria-logs-collector`.

## Chaos Mesh

**Chaos Mesh** — платформа для chaos engineering в Kubernetes. Позволяет внедрять сбои (network, pod, I/O, time, DNS, JVM, HTTP) для тестирования отказоустойчивости Kafka и приложений. Манифесты взяты из [strimzi-kafka-chaos-testing](https://github.com/patsevanton/strimzi-kafka-chaos-testing) и адаптированы под namespace `kafka-cluster` и кластер `kafka-cluster`.

### Установка Chaos Mesh

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh \
  --create-namespace \
  -f chaos-mesh/chaos-mesh-values.yaml \
  --version 2.8.1 \
  --wait
```

Проверка: `kubectl get pods -n chaos-mesh`

Для сбора метрик Chaos Mesh через VictoriaMetrics K8s Stack примените VMServiceScrape (в кластере используются CRD VictoriaMetrics, не Prometheus ServiceMonitor):

```bash
kubectl apply -f chaos-mesh/chaos-mesh-vmservicescrape.yaml
```

### Доступ к Dashboard

Dashboard использует RBAC-токен. Создайте ServiceAccount и токен:

```bash
kubectl apply -f chaos-mesh/chaos-mesh-rbac.yaml
sleep 3
kubectl get secret chaos-mesh-admin-token -n chaos-mesh -o jsonpath='{.data.token}' | base64 -d; echo
```

Скопируйте токен и войдите в Chaos Mesh Dashboard. В `chaos-mesh-values.yaml` задан Ingress-хост `chaos-dashboard.apatsev.org.ru` (при необходимости измените под свой домен).

### Chaos-эксперименты

В директории **chaos-experiments/** лежат готовые эксперименты для Kafka, Schema Registry, Kafka UI и producer/consumer:

| Файл | Описание |
|------|----------|
| `pod-kill.yaml` | Убийство брокера (одноразово + Schedule каждые 5 мин) |
| `pod-failure.yaml` | Симуляция падения пода |
| `network-delay.yaml`, `network-partition.yaml`, `network-loss.yaml` | Сетевые задержки, изоляция, потеря пакетов |
| `cpu-stress.yaml`, `memory-stress.yaml` | Нагрузка на CPU и память |
| `io-chaos.yaml` | Задержки и ошибки дискового I/O |
| `time-chaos.yaml` | Смещение системного времени |
| `dns-chaos.yaml` | Ошибки DNS для брокеров и producer |
| `jvm-chaos.yaml` | GC, stress и исключения в JVM брокеров |
| `http-chaos.yaml` | Задержки/ошибки Schema Registry и Kafka UI |

Запуск одного эксперимента:

```bash
kubectl apply -f chaos-experiments/pod-kill.yaml
```

Проверка: `kubectl get podchaos,networkchaos,stresschaos,schedule -n kafka-cluster`

Остановка: `kubectl delete -f chaos-experiments/pod-kill.yaml` или `kubectl delete -f chaos-experiments/`

Подробное описание экспериментов, рисков и ожидаемого поведения — в **chaos-experiments/README.md**.

## Импорт дашбордов Grafana

### Дашборды Strimzi Kafka

Импорт JSON дашбордов через UI Grafana:

https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/metrics/grafana-dashboards/strimzi-kafka-exporter.json

https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/metrics/grafana-dashboards/strimzi-kafka.json

https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/metrics/grafana-dashboards/strimzi-kraft.json

https://github.com/strimzi/strimzi-kafka-operator/blob/main/packaging/examples/metrics/grafana-dashboards/strimzi-operators.json

### Дашборд Go-приложения (Producer/Consumer)

Дашборды в **dashboards/**:

- **kafka-go-app-metrics.json** — метрики Go-приложения (Producer/Consumer, Kafka, Schema Registry)
- **redis-delivery-verification.json** — Redis (Yandex Valkey), SLO и верификация доставки ([docs/delivery-verification-critique.md](docs/delivery-verification-critique.md))

Импорт: Grafana → Dashboards → Import → загрузить JSON. Дашборд Go-приложения включает панели для:
- **Producer метрики**: скорость отправки сообщений, latency, ошибки
- **Consumer метрики**: скорость получения сообщений, latency, lag, ошибки
- **Schema Registry метрики**: запросы, latency, ошибки, кэш
- **Connection метрики**: статус подключений, переподключения

Подробное описание панелей и инструкции по импорту — в **dashboards/README.md**.

## Удаление

Инструкции по удалению всех компонентов (Chaos Mesh, VictoriaMetrics K8s Stack, Kafka, Strimzi, namespaces) вынесены в **uninstall.md**. Порядок удаления критичен из‑за finalizers у Strimzi CRD.

Автоматизированный скрипт:

```bash
./uninstall.sh
```

