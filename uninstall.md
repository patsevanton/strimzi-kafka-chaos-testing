# Удаление компонентов

Инструкции по удалению всех компонентов в правильном порядке (критично из-за finalizers у Strimzi CRD).

## Порядок удаления

1. **Chaos Mesh** (эксперименты и сам Chaos Mesh)
2. **Producer/Consumer приложения**
3. **Kafka UI**
4. **Schema Registry**
5. **VictoriaLogs Collector**
6. **VictoriaLogs Cluster**
7. **Kafka кластер и Strimzi** (важно: сначала удалить Kafka CR, затем оператор)
8. **VictoriaMetrics K8s Stack**

## Команды удаления

### 1. Удаление Chaos Mesh экспериментов

```bash
# Удалить все эксперименты
kubectl delete -f chaos-experiments/ --ignore-not-found=true

# Проверить, что эксперименты удалены
kubectl get podchaos,networkchaos,stresschaos,schedule -n kafka-cluster
```

### 2. Удаление Producer/Consumer

```bash
helm uninstall kafka-producer -n kafka-producer
helm uninstall kafka-consumer -n kafka-consumer
kubectl delete namespace kafka-producer kafka-consumer --ignore-not-found=true
```

### 3. Удаление Kafka UI

```bash
helm uninstall kafka-ui -n kafka-ui
kubectl delete namespace kafka-ui --ignore-not-found=true
```

### 4. Удаление Schema Registry

```bash
kubectl delete -f schema-registry.yaml --ignore-not-found=true
kubectl delete namespace schema-registry --ignore-not-found=true
kubectl delete kafkauser schema-registry -n kafka-cluster --ignore-not-found=true
kubectl delete kafkatopic schemas-topic -n kafka-cluster --ignore-not-found=true
```

### 5. Удаление VictoriaLogs Collector

```bash
helm uninstall victoria-logs-collector -n victoria-logs-collector
kubectl delete namespace victoria-logs-collector --ignore-not-found=true
```

### 6. Удаление VictoriaLogs Cluster

```bash
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
kubectl delete namespace victoria-logs-cluster --ignore-not-found=true
```

### 7. Удаление Kafka кластера и Strimzi

**Важно:** Сначала удалить все Kafka CR (Kafka, KafkaTopic, KafkaUser), затем оператор.

```bash
# Удалить все Kafka CR
kubectl delete kafka kafka-cluster -n kafka-cluster --ignore-not-found=true
kubectl delete kafkatopic test-topic schemas-topic -n kafka-cluster --ignore-not-found=true
kubectl delete kafkauser myuser schema-registry kafka-ui-user -n kafka-cluster --ignore-not-found=true

# Дождаться удаления Kafka (может занять несколько минут из-за finalizers)
kubectl wait kafka/kafka-cluster -n kafka-cluster --for=delete --timeout=300s || true

# Удалить метрики и другие ресурсы
kubectl delete -f strimzi/kafka-exporter-servicemonitor.yaml --ignore-not-found=true
kubectl delete -f strimzi/kafka-producer-metrics.yaml --ignore-not-found=true
kubectl delete -f strimzi/kafka-consumer-metrics.yaml --ignore-not-found=true
kubectl delete -f strimzi/cluster-operator-metrics.yaml --ignore-not-found=true
kubectl delete -f strimzi/entity-operator-metrics.yaml --ignore-not-found=true
kubectl delete -f strimzi/kafka-resources-metrics.yaml --ignore-not-found=true
kubectl delete -f strimzi/kube-state-metrics-ksm.yaml --ignore-not-found=true
kubectl delete -f strimzi/kube-state-metrics-configmap.yaml --ignore-not-found=true
kubectl delete -f strimzi/kafka-pdb.yaml --ignore-not-found=true

# Удалить Strimzi Operator
helm uninstall strimzi-cluster-operator -n strimzi
kubectl delete namespace strimzi --ignore-not-found=true
kubectl delete namespace kafka-cluster --ignore-not-found=true
```

### 8. Удаление VictoriaMetrics K8s Stack

```bash
helm uninstall vmks -n vmks
kubectl delete namespace vmks --ignore-not-found=true
```

### 9. Удаление Chaos Mesh

```bash
kubectl delete -f chaos-mesh/chaos-mesh-vmservicescrape.yaml --ignore-not-found=true
kubectl delete -f chaos-mesh/chaos-mesh-rbac.yaml --ignore-not-found=true
helm uninstall chaos-mesh -n chaos-mesh
kubectl delete namespace chaos-mesh --ignore-not-found=true
```

## Примечания

- Если ресурсы не удаляются из-за finalizers, проверьте логи операторов
- PVC для Kafka и VictoriaLogs не удаляются автоматически — удалите их вручную при необходимости
- Secrets в namespace `kafka-cluster` создаются Strimzi и удаляются вместе с KafkaUser
