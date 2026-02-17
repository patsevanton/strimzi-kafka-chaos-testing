# Chaos Experiments для Kafka

Примеры chaos-экспериментов для тестирования отказоустойчивости Strimzi Kafka кластера. Адаптированы под namespace `kafka-cluster` и кластер `kafka-cluster`.

## Предварительные требования

1. Установленный Chaos Mesh (см. основной README.md)
2. Запущенный Kafka кластер в namespace `kafka-cluster`
3. Запущенные producer и consumer приложения

## Типы экспериментов

| Файл | Тип | Описание |
|------|-----|----------|
| `pod-kill.yaml` | PodChaos + Schedule | Убийство брокера (одноразово + каждые 5 мин) |
| `pod-failure.yaml` | PodChaos | Симуляция падения пода |
| `network-delay.yaml` | NetworkChaos | Сетевые задержки 100–500 ms |
| `cpu-stress.yaml` | StressChaos | Нагрузка на CPU |
| `memory-stress.yaml` | StressChaos | Нагрузка на память |
| `io-chaos.yaml` | IOChaos | Задержки и ошибки дискового I/O |
| `time-chaos.yaml` | TimeChaos | Смещение системного времени |
| `jvm-chaos.yaml` | JVMChaos | GC, CPU/memory stress, latency, exception в JVM |
| `http-chaos.yaml` | HTTPChaos | Задержки/ошибки Schema Registry и Kafka UI |
| `network-partition.yaml` | NetworkChaos | Изоляция брокера / partition между брокерами и producer |
| `network-loss.yaml` | NetworkChaos | Потеря пакетов 10–30% |
| `dns-chaos.yaml` | DNSChaos | Ошибки DNS (брокеры, producer) |

## Запуск экспериментов

```bash
# Один эксперимент
kubectl apply -f chaos-experiments/pod-kill.yaml

# Все эксперименты (осторожно: множество хаос-эффектов одновременно)
kubectl apply -f chaos-experiments/pod-kill.yaml
kubectl apply -f chaos-experiments/pod-failure.yaml
kubectl apply -f chaos-experiments/network-delay.yaml
kubectl apply -f chaos-experiments/cpu-stress.yaml
kubectl apply -f chaos-experiments/memory-stress.yaml
kubectl apply -f chaos-experiments/io-chaos.yaml
kubectl apply -f chaos-experiments/time-chaos.yaml
kubectl apply -f chaos-experiments/jvm-chaos.yaml
kubectl apply -f chaos-experiments/http-chaos.yaml
kubectl apply -f chaos-experiments/network-partition.yaml
kubectl apply -f chaos-experiments/network-loss.yaml
kubectl apply -f chaos-experiments/dns-chaos.yaml
```

## Проверка статуса

```bash
kubectl get podchaos,networkchaos,stresschaos,iochaos,timechaos,jvmchaos,httpchaos,dnschaos,schedule -n kafka-cluster
kubectl get dnschaos -n kafka-producer
kubectl get httpchaos -n schema-registry
kubectl get httpchaos -n kafka-ui
```

## Остановка экспериментов

```bash
kubectl delete -f chaos-experiments/pod-kill.yaml
# или удалить все по типу:
kubectl delete podchaos --all -n kafka-cluster
kubectl delete -f chaos-experiments/
```

Подробное описание рисков и ожидаемого поведения - в [исходном README](https://github.com/patsevanton/strimzi-kafka-chaos-testing/tree/main/chaos-experiments).
