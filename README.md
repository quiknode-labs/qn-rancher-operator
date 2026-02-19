# QN Rancher Operator

A Kubernetes controller that automatically assigns namespaces to Rancher Projects based on the `appOwner` label.

## Overview

This controller watches for namespaces with an `appOwner` label and automatically assigns them to the corresponding Rancher Project. For example, if a namespace has `appOwner=DevOps`, the controller will find the "DevOps" project in Rancher and assign the namespace to it by adding the necessary labels and annotations.

## How It Works

1. The controller watches all namespaces in the cluster
2. When a namespace has the `appOwner` label, it searches for a Rancher Project with a matching display name
3. Once found, it adds the following to the namespace:
   - Label: `field.cattle.io/projectId: <project-id>`
   - Label: `field.cattle.io/clusterId: <cluster-id>` (if available)
   - Annotation: `field.cattle.io/projectId: <project-id>`

## Prerequisites

- Kubernetes cluster joined to Rancher
- Access to Rancher's management API (management.cattle.io/v3)
- RBAC permissions to:
  - List, watch, get, update, and patch namespaces
  - List and watch Rancher Projects (management.cattle.io/v3)

## Installation

### Option 1: Using Helm Chart (Recommended)

```bash
# Install using the Helm chart
helm install qn-rancher-operator ./charts/qn-rancher-operator \
  --set image.repository=ghcr.io/quiknode-labs/qn-rancher-operator \
  --set image.tag=latest \
  --namespace qn-rancher-operator-system \
  --create-namespace
```

### Option 2: Using Make (Development)

```bash
# Build the binary
make build

# Or build the Docker image
make docker-build

# Deploy to cluster
make deploy
```

**Note:** Update the image in `config/manager/deployment.yaml` to point to your container registry before deploying.

### Option 3: Manual Deployment

```bash
# Create namespace
kubectl create namespace qn-rancher-operator-system

# Apply RBAC
kubectl apply -f config/rbac/

# Apply deployment (update image first)
kubectl apply -f config/manager/deployment.yaml
```

### 3. Verify Installation

```bash
# Check if the controller is running
kubectl get pods -n qn-rancher-operator-system

# Check logs
kubectl logs -n qn-rancher-operator-system deployment/qn-rancher-operator-controller-manager
```

## Usage

Simply add the `appOwner` label to any namespace:

```bash
kubectl label namespace my-namespace appOwner=DevOps
```

The controller will automatically:
1. Detect the label
2. Find the "DevOps" project in Rancher
3. Assign the namespace to that project

You can verify the assignment by checking the namespace labels:

```bash
kubectl get namespace my-namespace -o yaml
```

You should see:
- `field.cattle.io/projectId: <project-id>`
- `field.cattle.io/clusterId: <cluster-id>` (if available)

## Project Matching

The controller searches for Rancher Projects by:
1. **Display Name**: Checks `spec.displayName` field
2. **Labels**: Searches common label patterns like:
   - `project.cattle.io/name`
   - `cattle.io/projectName`
   - `field.cattle.io/projectName`
3. **Annotations**: Checks annotations for project name

The matching is case-insensitive.

## Configuration

The controller can be configured via command-line flags:

- `--metrics-bind-address`: Address for metrics endpoint (default: `:8080`)
- `--health-probe-bind-address`: Address for health probe (default: `:8081`)
- `--leader-elect`: Enable leader election (default: `false`)

## Development

### Running Locally

```bash
# Run the controller locally (requires kubeconfig)
make run
```

### Building

```bash
# Build binary
make build

# Build Docker image
make docker-build IMG=your-registry/qn-rancher-operator:tag

# Push Docker image
make docker-push IMG=your-registry/qn-rancher-operator:tag
```

### Testing

```bash
# Run tests
make test
```

## Troubleshooting

### Controller Can't Find Projects

If the controller can't find Rancher Projects, check:

1. **RBAC Permissions**: Ensure the service account has permissions to list projects:
   ```bash
   kubectl auth can-i list projects.management.cattle.io --as=system:serviceaccount:qn-rancher-operator-system:qn-rancher-operator-controller-manager
   ```

2. **Rancher API Access**: Verify that the management API is accessible from the cluster

3. **Project Names**: Ensure the project display name matches the `appOwner` label exactly (case-insensitive)

### Namespace Not Being Assigned

1. Check controller logs:
   ```bash
   kubectl logs -n qn-rancher-operator-system deployment/qn-rancher-operator-controller-manager
   ```

2. Verify the namespace has the `appOwner` label:
   ```bash
   kubectl get namespace <namespace-name> --show-labels
   ```

3. Check if the namespace is already assigned to a project (controller skips already-assigned namespaces)

## Uninstallation

### Using Helm

```bash
helm uninstall qn-rancher-operator --namespace qn-rancher-operator-system
```

### Using Make

```bash
make undeploy
```

### Manual Removal

```bash
kubectl delete -f config/manager/deployment.yaml
kubectl delete -f config/rbac/
kubectl delete -f config/namespace.yaml
```

## CI/CD

This repository includes a GitHub Actions workflow (`.github/workflows/build-and-publish.yml`) that automatically:

- Builds Docker images on push to main/master branches
- Publishes images to GitHub Container Registry (ghcr.io)
- Tags images with version tags, branch names, and SHA
- Supports multi-architecture builds (amd64, arm64)

The workflow is triggered on:
- Push to main/master branches
- Push of version tags (v*)
- Pull requests (builds but doesn't push)
- Manual workflow dispatch

Images are published to: `ghcr.io/quiknode-labs/qn-rancher-operator`

## License

[Add your license here]
