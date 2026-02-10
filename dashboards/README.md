# Grafana Dashboards

## Kafka Go App Metrics

Дашборд для мониторинга метрик Go-приложения (Producer/Consumer) для работы с Kafka.

### Импорт дашборда

1. Откройте Grafana (например, http://grafana.apatsev.org.ru)
2. Перейдите в **Dashboards** → **Import**
3. Загрузите файл `kafka-go-app-metrics.json` или вставьте его содержимое
4. Выберите datasource **VictoriaMetrics** (или другой Prometheus-совместимый datasource)
5. Нажмите **Import**

### Описание панелей

#### Producer метрики

- **Messages Sent Rate** — скорость отправки сообщений (сообщений/сек)
- **Bytes Sent Rate** — скорость отправки данных (байт/сек)
- **Total Messages Sent** — общее количество отправленных сообщений
- **Total Bytes Sent** — общий объём отправленных данных
- **Error Rate** — скорость ошибок
- **Message Send Latency** — задержка отправки сообщения (p50/p95/p99)
- **Message Encode Latency** — задержка кодирования в Avro (p50/p95/p99)
- **Errors by Type** — ошибки по типам (encode, send, connection)

#### Consumer метрики

- **Messages Received Rate** — скорость получения сообщений (сообщений/сек)
- **Bytes Received Rate** — скорость получения данных (байт/сек)
- **Total Messages Received** — общее количество полученных сообщений
- **Error Rate** — скорость ошибок
- **Message Processing Latency** — задержка обработки сообщения (p50/p95/p99)
- **Message Decode Latency** — задержка декодирования из Avro (p50/p95/p99)
- **End-to-End Latency** — полная задержка от создания до потребления (p50/p95/p99)
- **Errors by Type** — ошибки по типам (read, decode, connection)
- **Consumer Lag** — отставание consumer (разница между последним offset и offset consumer)

#### Schema Registry метрики

- **Request Rate** — скорость запросов к Schema Registry
- **Request Latency** — задержка запросов (p50/p95/p99)
- **Errors by Type** — ошибки по типам (timeout, not_found, invalid_schema, network)
- **Cache Hit Rate** — процент попаданий в кэш схем

#### Connection метрики

- **Kafka Connection Status** — статус подключения к Kafka (1 = подключено, 0 = отключено)
- **Schema Registry Connection Status** — статус подключения к Schema Registry
- **Kafka Reconnections Rate** — скорость переподключений к Kafka

### Требования

- VictoriaMetrics или другой Prometheus-совместимый datasource
- Метрики должны собираться через VMServiceScrape (см. `strimzi/kafka-producer-metrics.yaml` и `strimzi/kafka-consumer-metrics.yaml`)

### Переменные

- `DS_VICTORIAMETRICS` — datasource для метрик (автоматически определяется при импорте)
