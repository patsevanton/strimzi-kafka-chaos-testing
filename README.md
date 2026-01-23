# Victoria stack в Kubernetes

Ниже оставлены только **описание и установка** для:

- `VictoriaLogs`
- `victoria-logs-collector`
- `VictoriaMetrics` (через `victoria-metrics-k8s-stack`)

## VictoriaLogs

**VictoriaLogs** — высокопроизводительное хранилище логов от команды VictoriaMetrics. Оптимизировано для больших объёмов логов, поддерживает эффективное хранение “wide events” (множество полей в записи), быстрые полнотекстовые поиски и масштабирование. LogsQL поддерживается в VictoriaLogs datasource для Grafana.

### Установка: Single Node (рекомендуется для начала)

```bash
helm upgrade --install victoria-logs-single \
  oci://ghcr.io/victoriametrics/helm-charts/victoria-logs-single \
  --namespace victoria-logs \
  --create-namespace \
  --wait \
  --version 0.0.25 \
  --timeout 15m
```

Single node версия запускает один под, который объединяет все функции (ingest, storage, query). Это упрощает установку и требует меньше ресурсов. При необходимости можно перейти на кластерную версию без миграции данных.

### Установка: Cluster

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

Пример `victorialogs-cluster-values.yaml`:

```yaml
vlselect:
  ingress:
    enabled: true
    hosts:
      - name: victorialogs.apatsev.org.ru
        path:
          - /
        port: http
    ingressClassName: nginx
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
    tls:
      - hosts:
        - victorialogs.apatsev.org.ru
        secretName: victorialogs-tls
```

Удаление:

```bash
helm uninstall -n victoria-logs-cluster victoria-logs-cluster
```

## victoria-logs-collector

`victoria-logs-collector` — это Helm-чарт от VictoriaMetrics, развертывающий агент сбора логов (`vlagent`) как DaemonSet в Kubernetes-кластере для автоматического сбора логов со всех контейнеров и их репликации в VictoriaLogs-хранилища.

### Установка

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

Пример `victorialogs-collector-values.yaml`:

```yaml
# Настройки отправки логов во внешнее хранилище (VictoriaLogs)
remoteWrite:
  - url: http://victoria-logs-cluster-vlinsert.victoria-logs-cluster:9481
    headers:
      # Поля, которые будут проигнорированы и не сохранены в VictoriaLogs
      # Полезно для уменьшения объёма данных и шума
      VL-Ignore-Fields:
        - kubernetes.container_id # уникальный ID контейнера, часто меняется и не несет ценной информации
        - kubernetes.pod_ip # IP адрес пода, динамический и редко полезный для анализа логов
        - kubernetes.pod_labels.pod-template-hash # хэш шаблона Deployment ReplicaSet, используется для идентификации реплик, но избыточен

# Настройки collector: определяют, как извлекать сообщение лога из входных данных.
collector:
  # msgField: список полей, из которых извлекается основное сообщение лога (_msg в VictoriaLogs)
  msgField:
    - message
    - msg
    # Поле 'http.uri' для HTTP-запросов, если логи содержат URI как сообщение.
    - http.uri
```

## VictoriaMetrics (VM K8s Stack)

`victoria-metrics-k8s-stack` — Helm-чарт для установки стека метрик VictoriaMetrics в Kubernetes (включая Grafana).

### Установка

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

Пример `vmks-values.yaml`:

```yaml
grafana:
  plugins:
    - victoriametrics-logs-datasource
  ingress:
    ingressClassName: nginx
    enabled: true
    hosts:
      - grafana.apatsev.org.ru
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "false"
      cert-manager.io/cluster-issuer: letsencrypt-prod
    tls:
      - hosts:
          - grafana.apatsev.org.ru
        secretName: grafana-tls
defaultDatasources:
  extra:
    - name: victoriametrics-logs
      access: proxy
      type: victoriametrics-logs-datasource
      url: http://victoria-logs-cluster-vlselect.victoria-logs-cluster.svc.cluster.local:9471
      jsonData:
        maxLines: 1000
      version: 1
defaultRules:
  groups:
    etcd:
      create: false
kube-state-metrics:
  metricLabelsAllowlist:
    - pods=[*]
vmsingle:
  enabled: false
vmcluster:
  enabled: true
  ingress:
    select:
      enabled: true
      ingressClassName: nginx
      annotations:
        nginx.ingress.kubernetes.io/ssl-redirect: "false"
        cert-manager.io/cluster-issuer: letsencrypt-prod
      hosts:
        - vmselect.apatsev.org.ru
      tls:
        - secretName: victoriametrics-tls
          hosts:
            - vmselect.apatsev.org.ru
```

Пароль `admin` для Grafana:

```bash
kubectl get secret vmks-grafana -n vmks -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```
