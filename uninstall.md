# Удаление всех компонентов

> **ВАЖНО:** Порядок удаления критичен! Strimzi CRD-ресурсы (Kafka, KafkaTopic, KafkaUser)
> имеют finalizers, которые требуют работающего оператора для их обработки.
> Если удалить оператор раньше Kafka-ресурсов, удаление namespace зависнет.

```bash
# 1. Удаление chaos-экспериментов (ПЕРЕД удалением Chaos Mesh!)
kubectl delete -f chaos-experiments/ --ignore-not-found

# 2. Удаление Chaos Mesh
helm uninstall chaos-mesh -n chaos-mesh
kubectl delete -f chaos-mesh-rbac.yaml --ignore-not-found

# 3. Удаление Observability stack
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
helm uninstall victoria-logs-collector -n victoria-logs-collector
helm uninstall vmks -n vmks

# 4. Удаление приложений
helm uninstall kafka-producer -n kafka-producer
helm uninstall kafka-consumer -n kafka-consumer
helm uninstall kafka-ui -n kafka-ui
kubectl delete secret kafka-ui-user -n kafka-ui --ignore-not-found

# 5. Удаление Schema Registry
kubectl delete -f schema-registry.yaml --ignore-not-found
kubectl delete secret schema-registry -n schema-registry --ignore-not-found

# 6. Удаление ServiceMonitor и PDB
kubectl delete -f kafka-servicemonitor.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-pdb.yaml --ignore-not-found

# 7. Удаление Kafka ресурсов (ПОКА ОПЕРАТОР ЕЩЁ РАБОТАЕТ!)
# Это критично - оператор обрабатывает finalizers
kubectl delete -f strimzi/kafka-topic.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-topic-schemas.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-user.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-user-schema-registry.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-user-ui.yaml --ignore-not-found

# 8. Ждём удаления topics и users, затем удаляем cluster
kubectl delete -f strimzi/kafka-cluster.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-nodepool.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-metrics-config.yaml --ignore-not-found

# 9. Проверяем что все Kafka CRD удалены
kubectl get kafka,kafkatopic,kafkauser,kafkanodepool -A

# 10. ТОЛЬКО ПОСЛЕ удаления всех Kafka ресурсов - удаляем оператор
helm uninstall strimzi-cluster-operator -n strimzi

# 11. Удаление namespaces
kubectl delete namespace chaos-mesh victoria-logs-cluster victoria-logs-collector \
  vmks kafka-producer kafka-consumer kafka-ui schema-registry kafka-cluster strimzi \
  --ignore-not-found
```

## Устранение зависших namespace

Если namespace застрял в состоянии `Terminating`, проверьте что его блокирует:

```bash
# Проверить причину зависания
kubectl get namespace <namespace> -o json | jq '.status.conditions'

# Найти ресурсы с застрявшими finalizers
kubectl api-resources --verbs=list --namespaced -o name | \
  xargs -I {} kubectl get {} -n <namespace> --ignore-not-found

# Удалить finalizers вручную (пример для chaos-mesh ресурсов)
kubectl patch <resource-type> <resource-name> -n <namespace> \
  -p '{"metadata":{"finalizers":null}}' --type=merge
```
