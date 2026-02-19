# QN Rancher Operator Helm Chart

This Helm chart deploys the QN Rancher Operator controller to your Kubernetes cluster.

## ⚠️ Important: Deployment Location

**This operator MUST be deployed on the Rancher management cluster** (where Rancher itself is running), **NOT** on downstream clusters that have the Rancher agent (cattle) installed.

The operator requires access to Rancher's management API (`management.cattle.io/v3`), which is only available on the management cluster. It uses this API to manage namespaces and projects across all clusters registered with Rancher.

## Prerequisites

- **Rancher management cluster** (where Rancher is deployed)
- Kubernetes 1.19+
- Helm 3.0+
- Access to Rancher's management API (management.cattle.io/v3) - only available on management cluster
- RBAC permissions to create ClusterRole and ClusterRoleBinding

## Installation

### Add the chart repository (if using a chart repository)

```bash
helm repo add qn-rancher-operator https://your-chart-repo-url
helm repo update
```

### Install the chart

**Deploy on the Rancher management cluster:**

```bash
# Install with default values (on management cluster)
helm install qn-rancher-operator ./charts/qn-rancher-operator

# Install with custom values
helm install qn-rancher-operator ./charts/qn-rancher-operator \
  --set image.repository=ghcr.io/quiknode-labs/qn-rancher-operator \
  --set image.tag=v0.1.0

# Install from values file
helm install qn-rancher-operator ./charts/qn-rancher-operator -f my-values.yaml
```

## Configuration

The following table lists the configurable parameters and their default values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of controller replicas | `1` |
| `image.repository` | Container image repository | `ghcr.io/quiknode-labs/qn-rancher-operator` |
| `image.tag` | Container image tag | `""` (uses chart appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.name` | Service account name | `""` (auto-generated) |
| `podAnnotations` | Annotations to add to pods | `{}` |
| `podSecurityContext` | Pod security context | `{}` |
| `securityContext` | Container security context | `{}` |
| `resources.limits` | Resource limits | `cpu: 500m, memory: 128Mi` |
| `resources.requests` | Resource requests | `cpu: 10m, memory: 64Mi` |
| `controller.leaderElection` | Enable leader election | `true` |
| `controller.metricsBindAddress` | Metrics server bind address | `:8080` |
| `controller.healthProbeBindAddress` | Health probe bind address | `:8081` |
| `rbac.create` | Create RBAC resources | `true` |
| `service.create` | Create service for metrics | `false` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `8080` |
| `autoscaling.enabled` | Enable HPA | `false` |
| `autoscaling.minReplicas` | Minimum replicas | `1` |
| `autoscaling.maxReplicas` | Maximum replicas | `3` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |

## Example values.yaml

```yaml
replicaCount: 1

image:
  repository: ghcr.io/quiknode-labs/qn-rancher-operator
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

controller:
  leaderElection: true
  metricsBindAddress: ":8080"
  healthProbeBindAddress: ":8081"

rbac:
  create: true
```

## Upgrading

```bash
helm upgrade qn-rancher-operator ./charts/qn-rancher-operator
```

## Uninstallation

```bash
helm uninstall qn-rancher-operator
```

## Troubleshooting

### Check controller logs

```bash
kubectl logs -l app.kubernetes.io/name=qn-rancher-operator
```

### Check RBAC permissions

```bash
kubectl auth can-i list projects.management.cattle.io \
  --as=system:serviceaccount:default:qn-rancher-operator
```

### Verify controller is running

```bash
kubectl get pods -l app.kubernetes.io/name=qn-rancher-operator
```

## License

[Add your license here]
