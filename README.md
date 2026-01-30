# Тестирование Strimzi Kafka под высокой нагрузкой

Проект для тестирования отказоустойчивости и производительности высоконагруженного кластера Apache Strimzi Kafka в Kubernetes. Включает инструменты для хаос-тестирования через Chaos Mesh, мониторинг через VictoriaMetrics, Schema Registry для управления схемами данных, Kafka UI — веб-интерфейc для просмотра топиков, сообщений, consumer groups, брокеров, а также примеры producer и consumer приложений на Go.

## Содержание

- [Prometheus CRDs](#prometheus-crds)
- [VictoriaMetrics (VM K8s Stack)](#victoriametrics-vm-k8s-stack)
- [Strimzi](#strimzi)
  - [Установка Strimzi](#установка-strimzi)
  - [Развертывание Kafka кластера](#развертывание-kafka-кластера)
  - [PodDisruptionBudget для Kafka](#poddisruptionbudget-для-kafka)
  - [ServiceMonitor для Kafka метрик](#servicemonitor-для-kafka-метрик)
  - [Создание Kafka топиков](#создание-kafka-топиков)
  - [Создание Kafka пользователей и секретов](#создание-kafka-пользователей-и-секретов)
  - [Schema Registry (Karapace) для Avro](#schema-registry-karapace-для-avro)
- [Producer App и Consumer App](#producer-app-и-consumer-app)
  - [Используемые библиотеки](#используемые-библиотеки)
  - [Сборка и публикация Docker образа](#сборка-и-публикация-docker-образа)
  - [Переменные окружения](#переменные-окружения)
  - [Запуск Producer/Consumer в кластере используя Helm](#запуск-producerconsumer-в-кластере-используя-helm)
- [Kafka UI и Observability](#kafka-ui-и-observability)
  - [Kafka UI (Kafbat UI)](#kafka-ui-kafbat-ui)
  - [Observability Stack](#observability-stack)
    - [VictoriaLogs](#victorialogs)
    - [victoria-logs-collector](#victoria-logs-collector)
  - [Формат сообщений](#формат-сообщений)
- [Chaos Mesh](#chaos-mesh)
  - [Установка Chaos Mesh](#установка-chaos-mesh)
  - [Настройка аутентификации Dashboard](#настройка-аутентификации-dashboard)
  - [Запуск всех Chaos-экспериментов](#запуск-всех-chaos-экспериментов)
- [Удаление (Helm / приложения / Strimzi / Kafka)](#удаление-helm--приложения--strimzi--kafka)

## Prometheus CRDs

Перед установкой любых компонентов мониторинга (VictoriaMetrics, ServiceMonitor, PodMonitor и др.) необходимо установить Prometheus CRDs (Custom Resource Definitions). Эти CRDs используются для определения ресурсов мониторинга.

**Важно**: Устанавливайте Prometheus CRDs **в самом начале**, до установки Strimzi, Kafka и других компонентов.

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm upgrade --install prometheus-operator-crds prometheus-community/prometheus-operator-crds \
  --namespace prometheus-crds \
  --create-namespace \
  --wait \
  --version 19.1.0
```

Проверка установки CRDs:

```bash
kubectl get crds | grep monitoring.coreos.com
```

Ожидаемый вывод:

```
alertmanagerconfigs.monitoring.coreos.com
alertmanagers.monitoring.coreos.com
podmonitors.monitoring.coreos.com
probes.monitoring.coreos.com
prometheusagents.monitoring.coreos.com
prometheuses.monitoring.coreos.com
prometheusrules.monitoring.coreos.com
scrapeconfigs.monitoring.coreos.com
servicemonitors.monitoring.coreos.com
thanosrulers.monitoring.coreos.com
```

## VictoriaMetrics (VM K8s Stack)

**[victoria-metrics-k8s-stack](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-metrics-k8s-stack)** — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

**Важно**: VictoriaMetrics устанавливается сразу после Prometheus CRDs, так как он предоставляет CRDs (VMServiceScrape, VMPodScrape и др.), которые используются другими компонентами (VictoriaLogs, collector и др.).

### Установка

Для установки используйте `victoriametrics-values.yaml` из репозитория.

**Важно**: Имя релиза и namespace `vmks` выбраны намеренно короткими, чтобы избежать ошибки `must be no more than 63 characters` для имён Kubernetes ресурсов (Service, ConfigMap и др.), которые формируются как `{release}-{chart}-{component}`.

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

**Параметры мониторинга** (default values):
- `victoria-metrics-operator.enabled: true` — включает оператор VictoriaMetrics
- `victoria-metrics-operator.serviceMonitor.enabled: true` — ServiceMonitor для оператора
- Автоматически конвертирует Prometheus ServiceMonitor/PodMonitor в VMServiceScrape/VMPodScrape
- Включает scrape конфигурации для kubelet, kube-proxy и других компонентов кластера

**Примечание о конфигурации Grafana dashboards**: В `victoriametrics-values.yaml` установлено `grafana.sidecar.dashboards.enabled: false`, потому что Helm-чарт не позволяет использовать одновременно sidecar и секцию `grafana.dashboards`. При использовании `grafana.dashboards` для загрузки дашбордов напрямую (по URL или gnetId) необходимо отключить sidecar.

**Примечание о Node Exporter**: `prometheus-node-exporter` разворачивается как DaemonSet и должен запускаться на каждой ноде. Если поды остаются в статусе `Pending`, проверьте:
- Наличие taints на нодах (node exporter по умолчанию имеет tolerations для master/control-plane)
- Достаточность ресурсов на нодах
- nodeSelector в values файле

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```

## Strimzi

**[Strimzi](https://github.com/strimzi/strimzi-kafka-operator)** — оператор Kubernetes для развертывания и управления Apache Kafka в Kubernetes. Предоставляет Custom Resource Definitions (CRDs) для управления Kafka-кластерами, топиками, пользователями и подключениями.

В Данном тестировании Kafka использует **KRaft (Kafka Raft)** — новый механизм управления метаданными в Apache Kafka, который заменяет зависимость от ZooKeeper. KRaft упрощает архитектуру кластера, улучшает производительность и масштабируемость, а также снижает задержки при управлении метаданными.

### Установка Strimzi

Namespace должен существовать заранее, если вы добавляете его в watchNamespaces
```bash
kubectl create namespace kafka-cluster --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install strimzi-cluster-operator \
  oci://quay.io/strimzi-helm/strimzi-kafka-operator \
  --namespace strimzi \
  --create-namespace \
  --set 'watchNamespaces={kafka-cluster}' \
  --set dashboards.enabled=true \
  --wait \
  --version 0.50.0
```

**Параметры мониторинга** (default values):
- `dashboards.enabled: false` — создание ConfigMap с Grafana dashboards для Strimzi (требует Grafana sidecar)

Проверка установки:

```bash
kubectl get pods -n strimzi
```

### Развертывание Kafka кластера

После установки оператора Strimzi можно развернуть Kafka кластер в режиме KRaft.

В этом репозитории уже есть готовые манифесты:

- `strimzi/kafka-cluster.yaml` — CR `Kafka` (с включёнными node pools через аннотацию `strimzi.io/node-pools: enabled` и KRaft через `strimzi.io/kraft: enabled`. **Включена SASL/SCRAM-SHA-512 аутентификация и ACL авторизация.**)
- `strimzi/kafka-nodepool.yaml` — CR `KafkaNodePool` (реплики/роли/хранилище)

Примечание: версия Strimzi из Helm-чарта в примере (`0.50.0`) поддерживает Kafka версии `4.x` (например `4.1.1`).

Важно: при включённых node pools (`strimzi.io/node-pools: enabled`) лучше сначала создать `KafkaNodePool`, а затем `Kafka`.
Иначе оператор Strimzi может логировать ошибку вида `KafkaNodePools are enabled, but no KafkaNodePools found...` до момента создания node pool.

```bash
kubectl apply -f strimzi/kafka-metrics-config.yaml
kubectl apply -f strimzi/kafka-nodepool.yaml
kubectl apply -f strimzi/kafka-cluster.yaml
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

Получить адрес bootstrap сервера
```bash
kubectl get svc -n kafka-cluster kafka-cluster-kafka-bootstrap -o jsonpath='{.metadata.name}.{.metadata.namespace}.svc.cluster.local:{.spec.ports[?(@.name=="tcp-clients")].port}'; echo
```

### PodDisruptionBudget для Kafka

PodDisruptionBudget (PDB) защищает кластер Kafka от чрезмерных нарушений во время плановых операций (rolling update, node drain и т.д.). Гарантирует, что как минимум 2 брокера всегда доступны.

```bash
kubectl apply -f strimzi/kafka-pdb.yaml
```

Проверка:

```bash
kubectl get pdb -n kafka-cluster
```

### ServiceMonitor для Kafka метрик

Для сбора метрик Kafka используются стандартные Prometheus CRDs (PodMonitor и ServiceMonitor):

```bash
kubectl apply -f kafka-servicemonitor.yaml
```

Проверка сбора метрик:

```bash
kubectl get podmonitor -n kafka-cluster
kubectl get servicemonitor -n kafka-cluster
```

### Создание Kafka топиков

Создайте Kafka топик через Strimzi KafkaTopic ресурс:

```bash
kubectl apply -f strimzi/kafka-topic.yaml
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
kubectl apply -f strimzi/kafka-user.yaml
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

Go-приложение из этого репозитория использует Avro и Schema Registry API. Для удобства здесь добавлены готовые манифесты для **[Karapace](https://github.com/Aiven-Open/karapace)** — open-source реализации API Confluent Schema Registry (drop-in replacement).

Karapace поднимается как обычный HTTP-сервис и хранит схемы в Kafka-топике `_schemas` (как и Confluent SR).

- `strimzi/kafka-topic-schemas.yaml` — KafkaTopic для `_schemas` (важно при `min.insync.replicas: 2`)
- `strimzi/kafka-user-schema-registry.yaml` — KafkaUser для Schema Registry с ACL для топика `_schemas`
- `schema-registry.yaml` — Service/Deployment для Karapace (`ghcr.io/aiven-open/karapace:5.0.3`). **Настроен на SASL/SCRAM-SHA-512 аутентификацию.**

```bash
kubectl create namespace schema-registry --dry-run=client -o yaml | kubectl apply -f -

# Создать топик для схем
kubectl apply -f strimzi/kafka-topic-schemas.yaml
kubectl wait kafkatopic/schemas-topic -n kafka-cluster --for=condition=Ready --timeout=120s

# Создать пользователя для Schema Registry (обязательно для SASL аутентификации)
kubectl apply -f strimzi/kafka-user-schema-registry.yaml
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

## Producer App и Consumer App

**Producer App и Consumer App** — Go приложение для работы с Apache Kafka через Strimzi. Приложение может работать в режиме producer (отправка сообщений) или consumer (получение сообщений) в зависимости от переменной окружения `MODE`. Используется для генерации нагрузки на кластер Kafka во время тестирования.

### Используемые библиотеки

- **[segmentio/kafka-go](https://github.com/segmentio/kafka-go)** — клиент для работы с Kafka
- **[riferrei/srclient](https://github.com/riferrei/srclient)** — клиент для Schema Registry API (совместим с Karapace)
- **[linkedin/goavro](https://github.com/linkedin/goavro)** — работа с Avro схемами
- **[xdg-go/scram](https://github.com/xdg-go/scram)** — SASL/SCRAM аутентификация (используется через kafka-go)

### Сборка и публикация Docker образа

Go-код в `main.go` можно изменять под свои нужды. После внесения изменений соберите и опубликуйте Docker образ:

```bash
# Сборка образа (используйте podman или docker)
podman build -t docker.io/antonpatsev/strimzi-kafka-chaos-testing:3.4.0 .

# Публикация в Docker Hub
podman push docker.io/antonpatsev/strimzi-kafka-chaos-testing:3.4.0
```

После публикации обновите версию образа в Helm values или передайте через `--set`:

```bash
helm upgrade --install kafka-producer ./helm/kafka-producer \
  --namespace kafka-producer \
  --create-namespace \
  --set image.repository="antonpatsev/strimzi-kafka-chaos-testing" \
  --set image.tag="3.4.0"
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
| `HEALTH_PORT` | Порт для health-проверок (liveness/readiness) | `8080` |

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

## Kafka UI и Observability

### Kafka UI (Kafbat UI)

**[Kafka UI](https://github.com/kafbat/kafka-ui)** — веб-интерфейс с открытым исходным кодом для управления и мониторинга Apache Kafka кластеров. Позволяет просматривать топики, сообщения, consumer groups, брокеры и конфигурации кластера через удобный графический интерфейс.

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
kubectl apply -f strimzi/kafka-user-ui.yaml
kubectl wait kafkauser/kafka-ui-user -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать секрет в namespace kafka-ui
kubectl create namespace kafka-ui --dry-run=client -o yaml | kubectl apply -f -

# Создать секрет с credentials для Kafka UI
kubectl get secret kafka-ui-user -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  jq '.data.username = ("kafka-ui-user" | @base64)' | \
  kubectl apply -n kafka-ui -f -

# Добавить репозиторий Helm
helm repo add kafbat-ui https://kafbat.github.io/helm-charts
helm repo update

# Получить default values из Helm chart (опционально, для ознакомления)
helm show values kafbat-ui/kafka-ui --version 1.4.2

# Развернуть Kafka UI через Helm
helm upgrade --install kafka-ui kafbat-ui/kafka-ui \
  --namespace kafka-ui \
  -f helm/kafka-ui-values.yaml \
  --version 1.4.2 \
  --wait
```

**Параметры мониторинга** (default values):
- Kafka UI не имеет встроенного ServiceMonitor в Helm chart
- Метрики доступны через Spring Boot Actuator на `/actuator/prometheus` (требует настройки в `yamlApplicationConfig`)
- Для сбора метрик создайте ServiceMonitor вручную

#### Проверка установки

```bash
# Проверить статус пода
kubectl get pods -n kafka-ui

# Проверить логи
kubectl logs -n kafka-ui -l app.kubernetes.io/name=kafka-ui --tail=100

# Получить сервис
kubectl get svc -n kafka-ui
```

#### Доступ к Kafka UI

Ingress уже настроен в `helm/kafka-ui-values.yaml`. По умолчанию используется хост `kafka-ui.apatsev.org.ru` с nginx ingress class.

Для изменения хоста отредактируйте секцию `ingress` в values файле или передайте через `--set`:

```bash
helm upgrade --install kafka-ui kafbat-ui/kafka-ui \
  --namespace kafka-ui \
  -f helm/kafka-ui-values.yaml \
  --set ingress.host=your-host.example.com
```

#### Диагностика проблем Kafka UI

### Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из компонентов кластера Kafka и приложений.

#### VictoriaLogs

**[VictoriaLogs](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/docs/victorialogs)** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

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
  -f victorialogs-cluster-values.yaml \
  --set vlselect.vmServiceScrape.enabled=true \
  --set vlinsert.vmServiceScrape.enabled=true \
  --set vlstorage.vmServiceScrape.enabled=true
```

**Параметры мониторинга** (default values):
- `vlselect.vmServiceScrape.enabled: false` — VMServiceScrape для vlselect компонента
- `vlinsert.vmServiceScrape.enabled: false` — VMServiceScrape для vlinsert компонента
- `vlstorage.vmServiceScrape.enabled: false` — VMServiceScrape для vlstorage компонента
- `*.vmServiceScrape.useServiceMonitor: false` — использовать ServiceMonitor вместо VMServiceScrape

#### victoria-logs-collector

**[victoria-logs-collector](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-logs-collector)** — Helm-чарт от VictoriaMetrics, разворачивающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилище.

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
  -f victorialogs-collector-values.yaml \
  --set podMonitor.enabled=true
```

**Параметры мониторинга** (default values):
- `podMonitor.enabled: false` — PodMonitor для сбора метрик collector
- `podMonitor.vm: false` — использовать VMPodScrape вместо PodMonitor

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

## Chaos Mesh

**[Chaos Mesh](https://github.com/chaos-mesh/chaos-mesh)** — платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

### Установка Chaos Mesh

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

**Параметры мониторинга** (default values):
- `chaosDaemon.service.scrape.enabled: true` — annotations для Prometheus scraping (включено по умолчанию)
- `prometheus.create: false` — встроенный Prometheus (не нужен при использовании VictoriaMetrics)
- `controllerManager.env.METRICS_PORT: 10080` — порт метрик controller-manager
- `dashboard.env.METRIC_PORT: 2334` — порт метрик dashboard

Проверка установки:

```bash
kubectl get pods -n chaos-mesh
```

Для сбора метрик Chaos Mesh через VictoriaMetrics/Prometheus Operator примените ServiceMonitor:

```bash
kubectl apply -f chaos-mesh-servicemonitor.yaml
```

### Настройка аутентификации Dashboard

Chaos Mesh Dashboard использует RBAC-токен для аутентификации. Для автоматического создания токена примените манифест `chaos-mesh-rbac.yaml`:

```bash
# Создать ServiceAccount, ClusterRole, ClusterRoleBinding и Secret с токеном
kubectl apply -f chaos-mesh-rbac.yaml

# Дождаться создания токена (несколько секунд)
sleep 3
```

Получение токена для входа в Dashboard:

```bash
# Получить токен из Secret
kubectl get secret chaos-mesh-admin-token -n chaos-mesh -o jsonpath='{.data.token}' | base64 -d; echo
```

Скопируйте полученный токен и используйте его для входа в Chaos Mesh Dashboard.

**Примечание**: Этот ServiceAccount имеет права администратора (Manager) на уровне всего кластера для управления всеми chaos-экспериментами.

### Запуск всех Chaos-экспериментов

В директории `chaos-experiments/` находятся готовые эксперименты для тестирования отказоустойчивости Kafka:

| Файл | Тип | Описание |
|------|-----|----------|
| `pod-kill.yaml` | PodChaos + Schedule | Убийство брокера Kafka (одноразовое + каждые 5 мин) |
| `pod-failure.yaml` | PodChaos | Симуляция падения пода |
| `network-delay.yaml` | NetworkChaos | Сетевые задержки 100-500ms |
| `network-partition.yaml` | NetworkChaos | Изоляция брокера от сети |
| `network-loss.yaml` | NetworkChaos | Потеря пакетов 10-30% |
| `cpu-stress.yaml` | StressChaos | Нагрузка на CPU |
| `memory-stress.yaml` | StressChaos | Нагрузка на память |
| `io-chaos.yaml` | IOChaos | Задержки и ошибки дисковых операций |
| `time-chaos.yaml` | TimeChaos | Смещение системного времени |
| `dns-chaos.yaml` | DNSChaos | Ошибки DNS резолвинга |
| `jvm-chaos.yaml` | JVMChaos | GC, memory/CPU stress в JVM |
| `http-chaos.yaml` | HTTPChaos | Ошибки HTTP для Schema Registry |

#### Запуск всех экспериментов

```bash
# Применить все эксперименты
kubectl apply -f chaos-experiments/pod-kill.yaml
kubectl apply -f chaos-experiments/pod-failure.yaml
kubectl apply -f chaos-experiments/network-delay.yaml
kubectl apply -f chaos-experiments/network-partition.yaml
kubectl apply -f chaos-experiments/network-loss.yaml
kubectl apply -f chaos-experiments/cpu-stress.yaml
kubectl apply -f chaos-experiments/memory-stress.yaml
kubectl apply -f chaos-experiments/io-chaos.yaml
kubectl apply -f chaos-experiments/time-chaos.yaml
kubectl apply -f chaos-experiments/dns-chaos.yaml
kubectl apply -f chaos-experiments/jvm-chaos.yaml
kubectl apply -f chaos-experiments/http-chaos.yaml
```

#### Проверка статуса экспериментов

```bash
# Проверить PodChaos эксперименты
kubectl get podchaos -n kafka-cluster

# Проверить NetworkChaos эксперименты
kubectl get networkchaos -n kafka-cluster

# Проверить StressChaos эксперименты
kubectl get stresschaos -n kafka-cluster

# Проверить IOChaos эксперименты
kubectl get iochaos -n kafka-cluster

# Проверить TimeChaos эксперименты
kubectl get timechaos -n kafka-cluster

# Проверить DNSChaos эксперименты
kubectl get dnschaos -n kafka-cluster
kubectl get dnschaos -n kafka-producer

# Проверить JVMChaos эксперименты
kubectl get jvmchaos -n kafka-cluster

# Проверить HTTPChaos эксперименты
kubectl get httpchaos -n schema-registry
kubectl get httpchaos -n kafka-ui

# Проверить Schedule (периодические эксперименты)
kubectl get schedule -n kafka-cluster

# Проверить все эксперименты
kubectl get podchaos,networkchaos,stresschaos,iochaos,timechaos,dnschaos,jvmchaos,schedule -n kafka-cluster
```

#### Остановка всех экспериментов

```bash
# Остановить все эксперименты
kubectl delete -f chaos-experiments/pod-kill.yaml
kubectl delete -f chaos-experiments/pod-failure.yaml
kubectl delete -f chaos-experiments/network-delay.yaml
kubectl delete -f chaos-experiments/network-partition.yaml
kubectl delete -f chaos-experiments/network-loss.yaml
kubectl delete -f chaos-experiments/cpu-stress.yaml
kubectl delete -f chaos-experiments/memory-stress.yaml
kubectl delete -f chaos-experiments/io-chaos.yaml
kubectl delete -f chaos-experiments/time-chaos.yaml
kubectl delete -f chaos-experiments/dns-chaos.yaml
kubectl delete -f chaos-experiments/jvm-chaos.yaml
kubectl delete -f chaos-experiments/http-chaos.yaml
```

Подробная документация в файле `chaos-experiments/README.md`.

## Удаление (Helm / приложения / Strimzi / Kafka)

Инструкции по удалению вынесены в отдельный файл: `uninstall.md`.
