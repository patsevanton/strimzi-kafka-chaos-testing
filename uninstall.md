# Удаление всех компонентов

```bash
helm uninstall chaos-mesh -n chaos-mesh
kubectl delete -f chaos-mesh-rbac.yaml
helm uninstall victoria-logs-cluster -n victoria-logs-cluster
helm uninstall victoria-logs-collector -n victoria-logs-collector
helm uninstall vmks -n vmks
helm uninstall kafka-producer -n kafka-producer
helm uninstall kafka-consumer -n kafka-consumer
kubectl delete -f schema-registry.yaml
kubectl delete secret schema-registry -n schema-registry
kubectl delete -f kafka-user-schema-registry.yaml
kubectl delete -f kafka-topic-schemas.yaml
helm uninstall kafka-ui -n kafka-ui
kubectl delete secret kafka-ui-user -n kafka-ui
kubectl delete -f kafka-user-ui.yaml
kubectl delete -f kafka-topic.yaml
kubectl delete -f kafka-user.yaml
kubectl delete -f kafka-cluster.yaml
kubectl delete -f kafka-nodepool.yaml
helm uninstall strimzi-cluster-operator -n strimzi
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
