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
| `pod-kill.yaml` | Убийство одного брокера | Низкий |
| `pod-failure.yaml` | Симуляция падения пода | Низкий-Средний |

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

## Как запустить эксперимент

```bash
# Применить один эксперимент
kubectl apply -f chaos-experiments/pod-kill.yaml

# Проверить статус эксперимента
kubectl get podchaos -n kafka-cluster
kubectl get networkchaos -n kafka-cluster
kubectl get stresschaos -n kafka-cluster

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

## Рекомендации по безопасности

1. **Не запускайте в production** без понимания последствий
2. Начинайте с экспериментов низкого риска
3. Убедитесь, что есть мониторинг и алерты
4. Имейте план отката (удаление эксперимента)
5. Ограничьте время эксперимента (duration)
6. Тестируйте по одному типу сбоя за раз
