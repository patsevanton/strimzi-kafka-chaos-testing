# Удаление всех компонентов

> **ВАЖНО:** Порядок удаления критичен! Strimzi CRD-ресурсы (Kafka, KafkaTopic, KafkaUser)
> имеют finalizers, которые требуют работающего оператора для их обработки.
> Если удалить оператор раньше Kafka-ресурсов, удаление namespace зависнет.

```bash
# 1. Удаление chaos-экспериментов ВО ВСЕХ namespace (ПЕРЕД удалением Chaos Mesh!)
kubectl delete -f chaos-experiments/ --ignore-not-found --wait=false
# Удаляем chaos ресурсы из kafka-cluster (могут быть активные эксперименты)
for resource in iochaos timechaos dnschaos networkchaos podchaos stresschaos; do
  kubectl delete $resource --all -n kafka-cluster --ignore-not-found --wait=false 2>/dev/null || true
done
kubectl wait --for=delete -f chaos-experiments/ --timeout=60s 2>/dev/null || true

# 2. Удаление Chaos Mesh (--wait для helm ждёт удаления ресурсов)
helm uninstall chaos-mesh -n chaos-mesh --wait --timeout=2m || true
kubectl delete -f chaos-mesh-rbac.yaml --ignore-not-found

# Очистка застрявших chaos-mesh finalizers если остались
for ns in kafka-cluster chaos-mesh; do
  for resource in iochaos timechaos dnschaos networkchaos podchaos stresschaos httpchaos jvmchaos; do
    kubectl get $resource -n $ns -o name 2>/dev/null | xargs -I {} kubectl patch {} -n $ns -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
  done
done

# 3. Удаление Observability stack (параллельно)
helm uninstall victoria-logs-cluster -n victoria-logs-cluster --wait --timeout=2m &
helm uninstall victoria-logs-collector -n victoria-logs-collector --wait --timeout=2m &
helm uninstall vmks -n vmks --wait --timeout=2m &
wait

# 4. Удаление приложений (параллельно)
helm uninstall kafka-producer -n kafka-producer --wait --timeout=2m &
helm uninstall kafka-consumer -n kafka-consumer --wait --timeout=2m &
helm uninstall kafka-ui -n kafka-ui --wait --timeout=2m &
wait
kubectl delete secret kafka-ui-user -n kafka-ui --ignore-not-found

# 5. Удаление Schema Registry
kubectl delete -f schema-registry.yaml --ignore-not-found --wait=false
kubectl wait --for=delete deployment -n schema-registry -l app=schema-registry --timeout=60s 2>/dev/null || true
kubectl delete secret schema-registry -n schema-registry --ignore-not-found

# 6. Удаление ServiceMonitor и PDB
kubectl delete -f kafka-servicemonitor.yaml --ignore-not-found
kubectl delete -f strimzi/kafka-pdb.yaml --ignore-not-found

# 7. Удаление Kafka ресурсов (ПОКА ОПЕРАТОР И CLUSTER ЕЩЁ РАБОТАЮТ!)
kubectl delete kafkatopic --all -n kafka-cluster --ignore-not-found --wait=false
kubectl delete kafkauser --all -n kafka-cluster --ignore-not-found --wait=false
# Ждём полного удаления topics/users ДО удаления cluster
kubectl wait --for=delete kafkatopic --all -n kafka-cluster --timeout=120s 2>/dev/null || true
kubectl wait --for=delete kafkauser --all -n kafka-cluster --timeout=120s 2>/dev/null || true

# 8. Если topics/users застряли - удаляем finalizers вручную
for resource in kafkatopic kafkauser; do
  kubectl get $resource -n kafka-cluster -o name 2>/dev/null | xargs -I {} kubectl patch {} -n kafka-cluster -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
done

# 9. Удаление Kafka cluster (только после удаления topics/users!)
kubectl delete kafka --all -n kafka-cluster --ignore-not-found --wait=false
kubectl delete kafkanodepool --all -n kafka-cluster --ignore-not-found --wait=false
kubectl wait --for=delete kafka --all -n kafka-cluster --timeout=180s 2>/dev/null || true
kubectl wait --for=delete kafkanodepool --all -n kafka-cluster --timeout=60s 2>/dev/null || true
kubectl delete -f strimzi/kafka-metrics-config.yaml --ignore-not-found

# 10. Проверяем что все Kafka CRD удалены
kubectl get kafka,kafkatopic,kafkauser,kafkanodepool -A

# 11. ТОЛЬКО ПОСЛЕ удаления всех Kafka ресурсов - удаляем оператор
helm uninstall strimzi-cluster-operator -n strimzi --wait --timeout=2m || true

# 12. Удаление всех оставшихся ресурсов в namespaces
for ns in chaos-mesh victoria-logs-cluster victoria-logs-collector vmks kafka-producer kafka-consumer kafka-ui schema-registry kafka-cluster strimzi; do
  kubectl delete all --all -n $ns --ignore-not-found --wait=false 2>/dev/null || true
done

# 13. Удаление namespaces (параллельно)
for ns in chaos-mesh victoria-logs-cluster victoria-logs-collector vmks kafka-producer kafka-consumer kafka-ui schema-registry kafka-cluster strimzi; do
  kubectl delete namespace $ns --ignore-not-found &
done
wait
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
