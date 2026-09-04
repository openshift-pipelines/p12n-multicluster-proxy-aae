# Proxy AAE - Kubernetes API Server for MultiKueue + Tekton

A Kubernetes Aggregated API Extension (AAE) that provides a unified API for accessing Tekton resources across multiple worker clusters managed by MultiKueue.

## Overview

This service exposes a manager-cluster-resident API that:
- Resolves worker clusters using Kueue Workload status
- Proxies read-only calls (TaskRuns, Pods, Pod status, and logs) to the appropriate worker cluster
- Supports both HTTP and WebSocket streaming for logs

## Features

- **Worker Cluster Resolution**: Uses Kueue Workload status to determine which worker cluster to proxy to
- **Authorization**: Validates access using TokenReview and SubjectAccessReview
- **Log Streaming**: Supports both HTTP fetch and WebSocket streaming for logs
- **Multi-Cluster Support**: Manages multiple worker clusters via kubeconfig secrets

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/resolve` | GET | Resolve worker cluster |
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/taskruns` | GET | List TaskRuns for PipelineRun |
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/pods` | GET | List Pods for PipelineRun |
| `/api/v1/namespaces/{ns}/pods/{pod}/status?pipelineRun={name}` | GET | Get Pod status |
| `/api/v1/namespaces/{ns}/logs?pipelineRun={name}&pod={pod}&container={container}` | GET | Fetch logs (HTTP) |
| `/api/v1/namespaces/{ns}/logs/stream?pipelineRun={name}&pod={pod}&container={container}` | GET | Stream logs (WebSocket) |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |

## Installation

### Prerequisites

- Kubernetes cluster with MultiKueue and Tekton installed
- Worker clusters already configured with MultiKueueCluster resources and secrets in `kueue-system` namespace

### Deploy with ko

You can install everything with a single command. Ensure `KO_DOCKER_REPO` is set.

```bash
export KO_DOCKER_REPO="kind.local"   # or your container repo
ko apply -R -f config/
```

Alternatively, use the Make target which wraps the same command:

```bash
make deploy
```

## Configuration

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | Port to listen on |
| `--workers-secret-namespace` | `kueue-system` | Namespace for worker kubeconfig secrets |
| `--request-timeout` | `30s` | Timeout for worker cluster requests |
| `--default-log-tail-lines` | `100` | Default number of log lines to tail |
| `--kubeconfig` | _(in-cluster)_ | Path to kubeconfig file |
| `--tls-cert` | | Path to TLS certificate file |
| `--tls-key` | | Path to TLS key file |
| `--hub-qps` | `50` | QPS rate limit for hub cluster client (TokenReview/SubjectAccessReview) |
| `--hub-burst` | `100` | Burst limit for hub cluster client (TokenReview/SubjectAccessReview) |
| `--client-qps` | `50` | QPS rate limit for worker cluster clients |
| `--client-burst` | `100` | Burst limit for worker cluster clients |

#### Rate limiting

Every incoming request requires two hub API calls (TokenReview + SubjectAccessReview), so the hub client's effective request throughput is roughly `hub-qps / 2` requests per second steady-state, bursting up to `hub-burst / 2`. The defaults (50 QPS / 100 burst) support ~25 req/s steady, bursting to ~50.

Worker cluster rate limits apply independently per cluster. Tune them based on the number of concurrent proxy requests that fan out to a single worker.

```bash
# Example: higher hub throughput for a busy cluster
go run ./cmd/proxy-server/main.go \
  --hub-qps=100 --hub-burst=200 \
  --client-qps=50 --client-burst=100
```

### Environment Variables

- `WORKERS_SECRET_NAMESPACE`: Namespace for worker kubeconfig secrets (default: `kueue-system`)

### Worker Cluster Configuration

Worker clusters are configured following the [Kueue MultiKueue pattern](https://kueue.sigs.k8s.io/docs/tasks/manage/setup_multikueue/):

1. **Secrets in kueue-system namespace**: Worker kubeconfigs are stored as secrets
2. **MultiKueueCluster resources**: Define the mapping between cluster names and secrets

```yaml
# 1. Create worker cluster secret
apiVersion: v1
kind: Secret
metadata:
  name: worker1-secret
  namespace: kueue-system
type: Opaque
stringData:
  kubeconfig: |
    <worker kubeconfig YAML>

---
# 2. Create MultiKueueCluster resource
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueCluster
metadata:
  name: multikueue-test-worker1
spec:
  kubeConfig:
    locationType: Secret
    location: worker1-secret
```

### Worker Cluster RBAC Configuration

The proxy service needs read permissions on the worker clusters to access Tekton resources. Apply the following RBAC configuration to **each worker cluster**:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: multikueue-tekton-reader
rules:
# Tekton Pipeline resources
- apiGroups: ["tekton.dev"]
  resources: ["pipelineruns", "taskruns", "pipelines", "tasks"]
  verbs: ["get", "list", "watch"]
# Core Kubernetes resources
- apiGroups: [""]
  resources: ["pods", "pods/log", "pods/status"]
  verbs: ["get", "list", "watch"]
# Events for debugging
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: multikueue-tekton-reader-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: multikueue-tekton-reader
subjects:
- kind: ServiceAccount
  name: multikueue-sa
  namespace: kueue-system
```

#### Applying RBAC to Worker Clusters

1. **Extract worker cluster kubeconfig:**
   ```bash
   kubectl get secret worker1-secret -n kueue-system -o jsonpath='{.data.kubeconfig}' | base64 -d > worker-kubeconfig
   ```

2. **Apply RBAC to worker cluster:**
   ```bash
   kubectl --kubeconfig=worker-kubeconfig apply -f worker-rbac.yaml
   ```

3. **Verify permissions:**
   ```bash
   kubectl --kubeconfig=worker-kubeconfig auth can-i list taskruns --as=system:serviceaccount:kueue-system:multikueue-sa
   ```

## Usage

### Port Forward Access

```bash
# Port forward to access the service
make port-forward
```

Refer to the API Endpoints table above for paths and methods. All endpoints (except `/health` and `/ready`) require an `Authorization: Bearer ${TOKEN}` header.

### Response Headers

All responses include the `X-Worker-Cluster` header indicating which worker cluster the data came from.

### Error Codes

- `401`: Unauthenticated
- `403`: Forbidden SubjectAccessReview(SAR) denied
- `404`: PipelineRun/Workload not found
- `409`: Not admitted (includes nominated clusters)
- `424`: Worker config missing/unreachable
- `502/504`: Upstream errors

### Authorization

All API endpoints (except `/health` and `/ready`) require a valid Kubernetes bearer token in the `Authorization` header:

```
Authorization: Bearer ${TOKEN}
```

The proxy authenticates the caller's bearer token via TokenReview and authorizes the request via SubjectAccessReview (SAR) against the hub cluster. Requests without a valid token or sufficient permissions will return `401 Unauthenticated` or `403 Forbidden`.

## Development

### Local Development

```bash
# Run locally
go run ./cmd/proxy-server/main.go --port=8080 --workers-secret-namespace=kueue-system

# With kubeconfig
go run ./cmd/proxy-server/main.go --kubeconfig=/path/to/kubeconfig
```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Architecture

The service consists of several components:

- **WorkloadResolver**: Resolves worker clusters from Kueue Workload status
- **WorkerConfigRegistry**: Manages worker cluster kubeconfigs
- **AuthzHandler**: Handles authentication (TokenReview) and authorization (SubjectAccessReview)
- **ProxyServer**: HTTP server that routes requests to appropriate worker clusters

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0.
