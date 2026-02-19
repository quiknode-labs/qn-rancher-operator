# Setting up GitHub Repository

This guide will help you push this repository to the `quiknode-labs` GitHub organization.

## Prerequisites

- Git installed
- GitHub CLI (`gh`) installed (optional, but recommended)
- Access to the `quiknode-labs` GitHub organization

## Steps

### 1. Initialize Git Repository

```bash
cd /Users/gonzalo/github/qn_rancher_operator
git init
```

### 2. Create the Repository on GitHub

#### Option A: Using GitHub CLI (Recommended)

```bash
# Authenticate if needed
gh auth login

# Create the repository in quiknode-labs org
gh repo create quiknode-labs/qn-rancher-operator \
  --public \
  --description "Kubernetes controller to automatically assign namespaces to Rancher Projects based on appOwner label" \
  --source=. \
  --remote=origin \
  --push
```

#### Option B: Using GitHub Web Interface

1. Go to https://github.com/organizations/quiknode-labs/repositories/new
2. Repository name: `qn-rancher-operator`
3. Description: "Kubernetes controller to automatically assign namespaces to Rancher Projects based on appOwner label"
4. Choose visibility (public/private)
5. **Do NOT** initialize with README, .gitignore, or license (we already have these)
6. Click "Create repository"

Then add the remote and push:

```bash
git remote add origin https://github.com/quiknode-labs/qn-rancher-operator.git
```

### 3. Add All Files and Make Initial Commit

```bash
# Add all files
git add .

# Make initial commit
git commit -m "Initial commit: QN Rancher Operator controller

- Kubernetes controller for automatic namespace-to-project assignment
- Case-insensitive project matching
- Automatic project creation
- Helm chart for deployment
- GitHub Actions workflow for CI/CD"
```

### 4. Push to GitHub

```bash
# Set default branch to main
git branch -M main

# Push to GitHub
git push -u origin main
```

### 5. Verify GitHub Actions

After pushing, check that the GitHub Actions workflow is set up correctly:

1. Go to: https://github.com/quiknode-labs/qn-rancher-operator/actions
2. The workflow should appear and may trigger automatically
3. Check that it has permission to write packages (for GHCR)

### 6. Configure Package Permissions (if needed)

If the workflow fails due to package permissions:

1. Go to: https://github.com/organizations/quiknode-labs/settings/actions
2. Under "Workflow permissions", ensure "Read and write permissions" is selected
3. Under "Packages", ensure the repository has write access

## Repository Structure

After pushing, your repository will have:

```
qn-rancher-operator/
├── .github/
│   └── workflows/
│       └── build-and-publish.yml    # CI/CD workflow
├── charts/
│   └── qn-rancher-operator/         # Helm chart
├── controllers/
│   └── namespace_controller.go      # Main controller logic
├── config/                           # Kubernetes manifests
├── main.go                           # Entry point
├── go.mod                            # Go dependencies
├── Dockerfile                         # Container image
├── Makefile                          # Build commands
└── README.md                          # Documentation
```

## Next Steps

1. **Tag a release** (optional):
   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```
   This will trigger the workflow to build and publish a versioned image.

2. **Install using Helm**:
   ```bash
   helm install qn-rancher-operator ./charts/qn-rancher-operator \
     --set image.repository=ghcr.io/quiknode-labs/qn-rancher-operator \
     --set image.tag=latest \
     --namespace qn-rancher-operator-system \
     --create-namespace
   ```

3. **Pull the image** (if needed):
   ```bash
   docker pull ghcr.io/quiknode-labs/qn-rancher-operator:latest
   ```

## Troubleshooting

### GitHub Actions not running

- Check that workflows are enabled in repository settings
- Verify the workflow file is in `.github/workflows/`
- Check Actions tab for any errors

### Image not found in GHCR

- Ensure the workflow completed successfully
- Check package visibility settings
- Verify the image name matches: `ghcr.io/quiknode-labs/qn-rancher-operator`

### Permission denied

- Ensure you have write access to the `quiknode-labs` organization
- Check that GitHub Actions has package write permissions
