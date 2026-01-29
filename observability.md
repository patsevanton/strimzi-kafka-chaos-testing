# Kafka UI, Chaos Mesh и Observability

## Kafka UI (Kafbat UI)

**Kafka UI** — веб-интерфейс с открытым исходным кодом для управления и мониторинга Apache Kafka кластеров. Позволяет просматривать топики, сообщения, consumer groups, брокеры и конфигурации кластера через удобный графический интерфейс.

GitHub: https://github.com/kafbat/kafka-ui

Основные возможности:
- Просмотр и управление топиками (создание, удаление, конфигурация)
- Просмотр сообщений в различных форматах (JSON, Avro, Protobuf)
- Мониторинг consumer groups и их лага
- Информация о брокерах и состоянии кластера
- Интеграция с Schema Registry
- RBAC и аутентификация

### Установка Kafka UI

```bash
# Создать пользователя для Kafka UI
kubectl apply -f kafka-user-ui.yaml
kubectl wait kafkauser/kafka-ui-user -n kafka-cluster --for=condition=Ready --timeout=120s

# Скопировать секрет в namespace kafka-ui
kubectl create namespace kafka-ui --dry-run=client -o yaml | kubectl apply -f -

# Создать секрет с credentials для Kafka UI
kubectl get secret kafka-ui-user -n kafka-cluster -o json | \
  jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.ownerReferences)' | \
  jq '.data.username = ("kafka-ui-user" | @base64)' | \
  kubectl apply -n kafka-ui -f -

# Развернуть Kafka UI
kubectl apply -f kafka-ui.yaml
kubectl rollout status deploy/kafka-ui -n kafka-ui --timeout=5m
```

### Проверка установки

```bash
# Проверить статус пода
kubectl get pods -n kafka-ui

# Проверить логи
kubectl logs -n kafka-ui -l app=kafka-ui --tail=100

# Получить сервис
kubectl get svc -n kafka-ui kafka-ui
```

### Доступ к Kafka UI

Для локального доступа через port-forward:

```bash
kubectl port-forward -n kafka-ui svc/kafka-ui 8080:8080
```

После этого откройте в браузере: http://localhost:8080

Для production-окружения рекомендуется настроить Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kafka-ui
  namespace: kafka-ui
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "50m"
spec:
  ingressClassName: nginx
  rules:
    - host: kafka-ui.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kafka-ui
                port:
                  number: 8080
```

### Диагностика проблем Kafka UI

Если Kafka UI не запускается:

```bash
# Проверить события
kubectl get events -n kafka-ui --sort-by=.lastTimestamp | tail -n 20

# Проверить логи
kubectl logs -n kafka-ui deploy/kafka-ui --all-containers --tail=200

# Проверить секрет
kubectl get secret kafka-ui-user -n kafka-ui -o yaml

# Проверить подключение к Kafka (из пода kafka-ui)
kubectl exec -n kafka-ui deploy/kafka-ui -- env | grep KAFKA
```

## Chaos Mesh

**Chaos Mesh** — платформа для chaos engineering в Kubernetes. Позволяет внедрять различные типы сбоев (network, pod, I/O, time и др.) для тестирования отказоустойчивости приложений.

### Установка Chaos Mesh

Для доступа к Dashboard через `ingress-nginx` используйте файл `chaos-mesh-values.yaml` из репозитория.

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh \
  --create-namespace \
  -f chaos-mesh-values.yaml \
  --wait
```

Проверка установки:

```bash
kubectl get pods -n chaos-mesh
```

## Observability Stack

Observability stack помогает отслеживать состояние системы во время тестирования, собирая логи и метрики из компонентов кластера Kafka и приложений.

### VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение "wide events" (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

#### Установка: Cluster

Для установки используйте `victorialogs-cluster-values.yaml` из репозитория.

```bash
helm upgrade --install victoria-logs-cluster \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-cluster \
  --namespace victoria-logs-cluster \
  --create-namespace \
  --wait \
  --version 0.0.25 \
  --timeout 15m \
  -f victorialogs-cluster-values.yaml
```

### victoria-logs-collector

`victoria-logs-collector` — Helm-чарт от VictoriaMetrics, разворачивающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилище.

#### Установка

Для установки используйте `victorialogs-collector-values.yaml` из репозитория.

```bash
helm upgrade --install victoria-logs-collector \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-collector \
  --namespace victoria-logs-collector \
  --create-namespace \
  --wait \
  --version 0.2.5 \
  --timeout 15m \
  -f victorialogs-collector-values.yaml
```

### VictoriaMetrics (VM K8s Stack)

`victoria-metrics-k8s-stack` — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

#### Установка

Для установки используйте `vmks-values.yaml` из репозитория.

```bash
helm upgrade --install vmks \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-metrics-k8s-stack \
  --namespace vmks \
  --create-namespace \
  --wait \
  --version 0.66.1 \
  --timeout 15m \
  -f vmks-values.yaml
```

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```
