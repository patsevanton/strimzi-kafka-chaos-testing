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

- **Messages Sent Rate** - скорость отправки сообщений (сообщений/сек)
- **Bytes Sent Rate** - скорость отправки данных (байт/сек)
- **Total Messages Sent** - общее количество отправленных сообщений
- **Total Bytes Sent** - общий объём отправленных данных
- **Error Rate** - скорость ошибок
- **Message Send Latency** - задержка отправки сообщения (p50/p95/p99)
- **Message Encode Latency** - задержка кодирования в Avro (p50/p95/p99)
- **Errors by Type** - ошибки по типам (encode, send, connection)

#### Consumer метрики

- **Messages Received Rate** - скорость получения сообщений (сообщений/сек)
- **Bytes Received Rate** - скорость получения данных (байт/сек)
- **Total Messages Received** - общее количество полученных сообщений
- **Error Rate** - скорость ошибок
- **Message Processing Latency** - задержка обработки сообщения (p50/p95/p99)
- **Message Decode Latency** - задержка декодирования из Avro (p50/p95/p99)
- **End-to-End Latency** - полная задержка от создания до потребления (p50/p95/p99)
- **Errors by Type** - ошибки по типам (read, decode, connection)
- **Consumer Lag** - отставание consumer (разница между последним offset и offset consumer)

#### Schema Registry метрики

- **Request Rate** - скорость запросов к Schema Registry
- **Request Latency** - задержка запросов (p50/p95/p99)
- **Errors by Type** - ошибки по типам (timeout, not_found, invalid_schema, network)

#### Connection метрики

- **Kafka Connection Status** - статус подключения к Kafka (1 = подключено, 0 = отключено)
- **Schema Registry Connection Status** - статус подключения к Schema Registry
- **Kafka Reconnections Rate** - скорость переподключений к Kafka

### Требования

- VictoriaMetrics или другой Prometheus-совместимый datasource
- Метрики должны собираться через VMServiceScrape (см. `strimzi/kafka-producer-metrics.yaml` и `strimzi/kafka-consumer-metrics.yaml`)

### Переменные

- `DS_VICTORIAMETRICS` - datasource для метрик (автоматически определяется при импорте)

---

## Redis & Delivery Verification

Дашборд для мониторинга Redis и верификации доставки по [docs/delivery-verification-critique.md](../docs/delivery-verification-critique.md).

### Импорт

1. Grafana → **Dashboards** → **Import** → загрузите `redis-delivery-verification.json`
2. Выберите datasource **VictoriaMetrics** → **Import**

### Что на дашборде

- **Redis:** connected clients, commands rate, memory, rejected connections, command latency (п.7, п.1 критикала).
- **Delivery verification (SLO):** pending messages, pending old (нарушение SLO по задержке), received rate, hash mismatch (п.4).
- **Pending vs Old:** рост pending без роста old - consumer отстаёт, но в рамках SLO.

### Требования

- Сбор метрик Redis: разверните redis-exporter для in-cluster Redis: `kubectl apply -f redis/redis-exporter-in-cluster.yaml`
- Метрики приложения (`redis_pending_messages`, `redis_pending_old_messages`, `kafka_consumer_redis_hash_mismatch_total` и т.д.) собираются существующими VMServiceScrape для producer/consumer.
