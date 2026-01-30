# Удаление всех компонентов

```bash
# Удаление chaos-экспериментов (если запущены)
kubectl delete -f chaos-experiments/ --ignore-not-found

# Удаление Chaos Mesh
helm uninstall chaos-mesh -n chaos-mesh
kubectl delete -f chaos-mesh-rbac.yaml

# Удаление Observability stack
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
helm uninstall victoria-logs-collector -n victoria-logs-collector
helm uninstall vmks -n vmks

# Удаление приложений
helm uninstall kafka-producer -n kafka-producer
helm uninstall kafka-consumer -n kafka-consumer

# Удаление Schema Registry
kubectl delete -f schema-registry.yaml
kubectl delete secret schema-registry -n schema-registry
kubectl delete -f strimzi/kafka-user-schema-registry.yaml
kubectl delete -f strimzi/kafka-topic-schemas.yaml

# Удаление Kafka UI
helm uninstall kafka-ui -n kafka-ui
kubectl delete secret kafka-ui-user -n kafka-ui
kubectl delete -f strimzi/kafka-user-ui.yaml

# Удаление ServiceMonitor и PDB
kubectl delete -f kafka-servicemonitor.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-pdb.yaml --ignore-not-found

# Удаление Kafka ресурсов
kubectl delete -f strimzi/kafka-topic.yaml
kubectl delete -f strimzi/kafka-user.yaml
kubectl delete -f strimzi/kafka-cluster.yaml
kubectl delete -f strimzi/kafka-nodepool.yaml

# Удаление Strimzi оператора
helm uninstall strimzi-cluster-operator -n strimzi

# Удаление namespaces
kubectl delete namespace chaos-mesh
kubectl delete namespace victoria-logs-cluster
kubectl delete namespace victoria-logs-collector
kubectl delete namespace vmks
kubectl delete namespace kafka-producer
kubectl delete namespace kafka-consumer
kubectl delete namespace kafka-ui
kubectl delete namespace schema-registry
kubectl delete namespace kafka-cluster
kubectl delete namespace strimzi
```
