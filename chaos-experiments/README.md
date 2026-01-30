# Chaos Experiments для Kafka

Примеры chaos-экспериментов для тестирования отказоустойчивости Strimzi Kafka кластера.

## Предварительные требования

1. Установленный Chaos Mesh (см. основной README.md)
2. Запущенный Kafka кластер в namespace `kafka-cluster`
3. Запущенные producer и consumer приложения

## Типы экспериментов

### Pod Chaos (нарушения работы подов)

| Файл | Описание | Риск |
|------|----------|------|
| `pod-kill.yaml` | Убийство одного брокера (одноразовое + Schedule каждые 5 мин) | Низкий |
| `pod-failure.yaml` | Симуляция падения пода | Низкий-Средний |

**Примечание**: Для периодических экспериментов используется `Schedule` CRD вместо устаревшего `spec.scheduler`.

### Network Chaos (сетевые нарушения)

| Файл | Описание | Риск |
|------|----------|------|
| `network-delay.yaml` | Сетевые задержки 100-500ms | Низкий |
| `network-partition.yaml` | Изоляция брокера от сети | Средний |
| `network-loss.yaml` | Потеря пакетов 10-30% | Средний |

### Stress Chaos (нагрузочное тестирование)

| Файл | Описание | Риск |
|------|----------|------|
| `cpu-stress.yaml` | Нагрузка на CPU 80-95% | Средний |
| `memory-stress.yaml` | Нагрузка на память | Средний |

### IO Chaos (дисковые нарушения)

| Файл | Описание | Риск |
|------|----------|------|
| `io-chaos.yaml` | Задержки I/O операций (100ms) | Средний |
| `io-chaos.yaml` | Ошибки I/O (EIO) | Средний |
| `io-chaos.yaml` | Симуляция заполненного диска (ENOSPC) | Средний |

**Примечание**: Все IO Chaos эксперименты безопасны для данных — они не повреждают данные, а только добавляют задержки или возвращают ошибки приложению.

### Time Chaos (манипуляции со временем)

| Файл | Описание | Риск |
|------|----------|------|
| `time-chaos.yaml` | Смещение времени вперёд на 5 минут | Средний |
| `time-chaos.yaml` | Смещение времени назад на 1 час | Средний |
| `time-chaos.yaml` | Смещение времени только на одном брокере | Высокий |

### DNS Chaos (DNS нарушения)

| Файл | Описание | Риск |
|------|----------|------|
| `dns-chaos.yaml` | Ошибки DNS резолвинга для брокеров | Средний |
| `dns-chaos.yaml` | Случайные DNS ответы | Высокий |
| `dns-chaos.yaml` | Ошибки DNS для producer приложения | Средний |

### JVM Chaos (JVM нарушения)

| Файл | Описание | Риск |
|------|----------|------|
| `jvm-chaos.yaml` | Принудительный GC | Низкий |
| `jvm-chaos.yaml` | CPU stress в JVM | Средний |
| `jvm-chaos.yaml` | Memory stress в JVM | Средний-Высокий |
| `jvm-chaos.yaml` | Задержка в методе append | Средний |
| `jvm-chaos.yaml` | Исключение в методе handleProduceRequest | Средний-Высокий |

**Примечание**: JVMChaos требует инъекции Chaos Mesh агента в JVM процесс.

### HTTP Chaos (HTTP нарушения)

| Файл | Описание | Риск |
|------|----------|------|
| `http-chaos.yaml` | Задержка ответов Schema Registry | Низкий |
| `http-chaos.yaml` | 500 ошибки Schema Registry | Средний |
| `http-chaos.yaml` | Обрыв соединений Schema Registry | Средний |
| `http-chaos.yaml` | Задержка ответов Kafka UI | Низкий |

## Как запустить эксперимент

```bash
# Применить один эксперимент
kubectl apply -f chaos-experiments/pod-kill.yaml

# Проверить статус эксперимента
kubectl get podchaos -n kafka-cluster
kubectl get networkchaos -n kafka-cluster
kubectl get stresschaos -n kafka-cluster
kubectl get iochaos -n kafka-cluster
kubectl get timechaos -n kafka-cluster
kubectl get dnschaos -n kafka-cluster
kubectl get dnschaos -n kafka-producer
kubectl get jvmchaos -n kafka-cluster
kubectl get httpchaos -n schema-registry
kubectl get httpchaos -n kafka-ui
kubectl get schedule -n kafka-cluster

# Посмотреть детали
kubectl describe podchaos kafka-pod-kill -n kafka-cluster
```

## Как остановить эксперимент

```bash
# Удалить конкретный эксперимент
kubectl delete -f chaos-experiments/pod-kill.yaml

# Или удалить все эксперименты определённого типа
kubectl delete podchaos --all -n kafka-cluster
kubectl delete networkchaos --all -n kafka-cluster
kubectl delete stresschaos --all -n kafka-cluster
kubectl delete iochaos --all -n kafka-cluster
kubectl delete timechaos --all -n kafka-cluster
kubectl delete dnschaos --all -n kafka-cluster
kubectl delete dnschaos --all -n kafka-producer
kubectl delete jvmchaos --all -n kafka-cluster
kubectl delete httpchaos --all -n schema-registry
kubectl delete httpchaos --all -n kafka-ui
kubectl delete schedule --all -n kafka-cluster

# Удалить все chaos-эксперименты
kubectl delete -f chaos-experiments/
```

## Что наблюдать во время эксперимента

### Логи приложений

```bash
# Producer logs - следить за ошибками отправки
kubectl logs -n kafka-producer -l app.kubernetes.io/name=kafka-producer -f

# Consumer logs - следить за задержками получения
kubectl logs -n kafka-consumer -l app.kubernetes.io/name=kafka-consumer -f
```

### Состояние Kafka кластера

```bash
# Статус подов
kubectl get pods -n kafka-cluster -l strimzi.io/cluster=kafka-cluster -w

# Статус Kafka CR
kubectl get kafka -n kafka-cluster -w

# Логи брокеров
kubectl logs -n kafka-cluster kafka-cluster-mixed-0 -f
```

### Grafana метрики

- Kafka broker availability
- Message throughput (in/out)
- Consumer lag
- Request latency (produce/fetch)
- Under-replicated partitions
- ISR shrink/expand events

## Ожидаемое поведение

### При убийстве одного брокера (pod-kill)
- Кластер остаётся доступным (2 из 3 брокеров)
- Leader election для затронутых партиций (~10-30 сек)
- Producer/Consumer переподключаются автоматически
- StatefulSet восстанавливает под

### При сетевых задержках (network-delay)
- Увеличение latency у producer/consumer
- Возможные таймауты при высоких задержках
- Сообщения не теряются (при правильных acks)

### При сетевом разделении (network-partition)
- Изолированный брокер теряет лидерство
- Возможны временные ошибки записи/чтения
- После восстановления сети - синхронизация данных

### При задержках I/O (io-latency)
- Увеличение latency для produce/fetch операций
- Возможные таймауты при записи/чтении
- Kafka может пометить брокер как медленный

### При ошибках I/O (io-fault)
- Брокер может упасть или перезапуститься
- Leader election для затронутых партиций
- Данные НЕ повреждаются (ошибка возвращается приложению)

### При симуляции заполненного диска (io-disk-full)
- Ошибки записи на брокере
- Producer получает ошибки
- Брокер может стать нездоровым (unhealthy)

### При смещении времени (time-chaos)
- Возможные проблемы с log segment rotation
- Некорректная работа retention policies
- Проблемы с timestamp-based операциями

### При ошибках DNS (dns-chaos)
- Невозможность обнаружить другие брокеры
- Ошибки подключения producer/consumer
- Проблемы с service discovery

### При JVM хаосе (jvm-chaos)
- GC: временные паузы, увеличение latency
- Memory stress: частые GC, возможный OOM
- Method latency: замедление конкретных операций

### При HTTP хаосе (http-chaos)
- Schema Registry delay: таймауты при получении схем
- Schema Registry error: producer не может отправить сообщения
- Connection abort: повторные попытки соединения

## Рекомендации по безопасности

1. **Не запускайте в production** без понимания последствий
2. Начинайте с экспериментов низкого риска
3. Убедитесь, что есть мониторинг и алерты
4. Имейте план отката (удаление эксперимента)
5. Ограничьте время эксперимента (duration)
6. Тестируйте по одному типу сбоя за раз
