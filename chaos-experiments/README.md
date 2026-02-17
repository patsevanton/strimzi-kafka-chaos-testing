# Chaos Experiments для Kafka

Примеры chaos-экспериментов для тестирования отказоустойчивости Strimzi Kafka кластера. Адаптированы под namespace `kafka-cluster` и кластер `kafka-cluster`.

**Установка стека, порядок развёртывания и пошаговый запуск экспериментов** — в [основном README](../README.md) (разделы Chaos Mesh, Запуск chaos-экспериментов).

## Типы экспериментов (CRD Chaos Mesh)

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

## Проверка статуса (все задействованные namespace)

```bash
kubectl get podchaos,networkchaos,stresschaos,iochaos,timechaos,jvmchaos,httpchaos,dnschaos,schedule -n kafka-cluster
kubectl get dnschaos -n kafka-producer
kubectl get httpchaos -n schema-registry
kubectl get httpchaos -n kafka-ui
```
