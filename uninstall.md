# Удаление всех компонентов

> **ВАЖНО:** Порядок критичен! Strimzi CRD имеют finalizers, требующие работающего оператора.

```bash
# 1. Chaos experiments и Chaos Mesh
kubectl delete -f chaos-experiments/ --ignore-not-found --wait=false
for r in iochaos timechaos dnschaos networkchaos podchaos stresschaos httpchaos jvmchaos; do
  kubectl delete $r --all -n kafka-cluster --ignore-not-found --wait=false 2>/dev/null || true
done
kubectl delete -f chaos-mesh-servicemonitor.yaml --ignore-not-found
helm uninstall chaos-mesh -n chaos-mesh --wait --timeout=2m || true
kubectl delete -f chaos-mesh-rbac.yaml --ignore-not-found

# 2. Observability и приложения (параллельно)
helm uninstall victorialogs-cluster -n victorialogs-cluster --wait --timeout=2m &
helm uninstall victoria-logs-collector -n victoria-logs-collector --wait --timeout=2m &
helm uninstall vmks -n vmks --wait --timeout=2m &
helm uninstall kafka-producer -n kafka-producer --wait --timeout=2m &
helm uninstall kafka-consumer -n kafka-consumer --wait --timeout=2m &
helm uninstall kafka-ui -n kafka-ui --wait --timeout=2m &
wait
kubectl delete -f schema-registry.yaml --ignore-not-found --wait=false
kubectl delete -f kafka-servicemonitor.yaml -f strimzi/kafka-pdb.yaml --ignore-not-found

# 3. Kafka ресурсы (ПОКА ОПЕРАТОР РАБОТАЕТ!)
kubectl delete kafkatopic,kafkauser --all -n kafka-cluster --ignore-not-found --wait=false
kubectl wait --for=delete kafkatopic,kafkauser --all -n kafka-cluster --timeout=120s 2>/dev/null || true
for r in kafkatopic kafkauser; do
  kubectl get $r -n kafka-cluster -o name 2>/dev/null | xargs -I {} kubectl patch {} -n kafka-cluster -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
done

# 4. Kafka cluster
kubectl delete kafka,kafkanodepool --all -n kafka-cluster --ignore-not-found --wait=false
kubectl wait --for=delete kafka --all -n kafka-cluster --timeout=180s 2>/dev/null || true
kubectl delete -f strimzi/kafka-metrics-config.yaml --ignore-not-found

# 5. Strimzi оператор (ПОСЛЕ удаления Kafka ресурсов!)
helm uninstall strimzi-cluster-operator -n strimzi --wait --timeout=2m || true

# 6. Prometheus CRDs (после удаления всех мониторинг ресурсов)
helm uninstall prometheus-operator-crds -n prometheus-crds --wait --timeout=2m || true

# 7. Namespaces
NS="chaos-mesh victorialogs-cluster victoria-logs-collector vmks kafka-producer kafka-consumer kafka-ui schema-registry kafka-cluster strimzi prometheus-crds"
for ns in $NS; do kubectl delete ns $ns --ignore-not-found & done; wait
```

## Зависшие namespace

```bash
kubectl get namespace <ns> -o json | jq '.status.conditions'
kubectl api-resources --verbs=list --namespaced -o name | xargs -I {} kubectl get {} -n <ns> --ignore-not-found
kubectl patch <type> <name> -n <ns> -p '{"metadata":{"finalizers":null}}' --type=merge
```
