package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// Rancher labels and annotations for project assignment
	rancherProjectIDLabel      = "field.cattle.io/projectId"
	rancherClusterIDLabel      = "field.cattle.io/clusterId"
	rancherProjectIDAnnotation = "field.cattle.io/projectId"

	// Label we use to determine project assignment
	appOwnerLabel = "appOwner"

	// Rancher Project resource
	// NOTE: This API is only available on the Rancher management cluster.
	// The operator MUST be deployed on the management cluster, not on downstream clusters.
	rancherProjectAPIVersion = "management.cattle.io/v3"
	rancherProjectKind       = "Project"
	rancherClusterAPIVersion = "management.cattle.io/v3"
	rancherClusterKind       = "Cluster"

	// Cluster refresh interval
	clusterRefreshInterval = 5 * time.Minute
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Manager       manager.Manager
	clusterClients map[string]client.Client
	clusterMutex   sync.RWMutex
	lastClusterRefresh time.Time
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=management.cattle.io,resources=projects,verbs=get;list;watch
//+kubebuilder:rbac:groups=management.cattle.io,resources=clusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Determine which cluster this namespace belongs to from the request
	// The request may contain cluster information in the namespace field or we need to detect it
	clusterID, namespaceClient := r.getClusterClient(ctx, req)
	if namespaceClient == nil {
		logger.V(1).Info("no cluster client available, using management cluster client", "namespace", req.Name)
		namespaceClient = r.Client
		clusterID = "local"
	}

	// Fetch the Namespace instance from the appropriate cluster
	namespace := &corev1.Namespace{}
	if err := namespaceClient.Get(ctx, types.NamespacedName{Name: req.Name}, namespace); err != nil {
		if errors.IsNotFound(err) {
			// Namespace was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Namespace", "clusterId", clusterID)
		return ctrl.Result{}, err
	}

	// Check if namespace has appOwner label
	appOwner, exists := namespace.Labels[appOwnerLabel]
	if !exists || appOwner == "" {
		logger.V(1).Info("namespace does not have appOwner label, skipping", "namespace", namespace.Name, "clusterId", clusterID)
		return ctrl.Result{}, nil
	}

	// Check if namespace is already assigned to a project
	if projectID, hasProject := namespace.Labels[rancherProjectIDLabel]; hasProject && projectID != "" {
		logger.V(1).Info("namespace already assigned to project", "namespace", namespace.Name, "projectId", projectID, "clusterId", clusterID)
		return ctrl.Result{}, nil
	}

	logger.Info("processing namespace with appOwner label", "namespace", namespace.Name, "appOwner", appOwner, "clusterId", clusterID)

	// Find the Rancher Project by name (case-insensitive)
	// Projects are managed on the management cluster, so use the management client
	project, err := r.findProjectByName(ctx, appOwner, clusterID)
	if err != nil {
		logger.Error(err, "unable to find project", "projectName", appOwner, "clusterId", clusterID)
		return ctrl.Result{}, err
	}

	// If project doesn't exist, skip (project creation removed)
	if project == nil {
		logger.Info("project not found, skipping namespace assignment", "projectName", appOwner, "namespace", namespace.Name, "clusterId", clusterID)
		return ctrl.Result{}, nil
	}

	// Get project ID and cluster ID from the project
	projectID := project.GetName()
	projectClusterID := r.extractClusterID(projectID)

	// Use the project's cluster ID if available, otherwise use the detected cluster ID
	if projectClusterID == "" {
		projectClusterID = clusterID
	}

	if projectID == "" {
		logger.Info("project ID is empty, skipping", "projectName", appOwner, "clusterId", clusterID)
		return ctrl.Result{}, nil
	}

	// Update namespace with project labels and annotations using the appropriate cluster client
	if err := r.updateNamespaceWithProject(ctx, namespaceClient, namespace, projectID, projectClusterID); err != nil {
		logger.Error(err, "unable to update namespace with project assignment", "namespace", namespace.Name, "clusterId", clusterID)
		return ctrl.Result{}, err
	}

	logger.Info("successfully assigned namespace to project", "namespace", namespace.Name, "projectId", projectID, "clusterId", projectClusterID)
	return ctrl.Result{}, nil
}

// getClusterClient determines which cluster client to use based on the request
// Returns the cluster ID and the appropriate client
// For now, we primarily watch the management cluster. Downstream cluster access
// will be handled through Rancher's cluster proxy when needed.
func (r *NamespaceReconciler) getClusterClient(ctx context.Context, req ctrl.Request) (string, client.Client) {
	r.clusterMutex.RLock()
	defer r.clusterMutex.RUnlock()

	// For now, we're watching the management cluster directly
	// In the future, we can enhance this to detect which cluster the namespace belongs to
	// by checking namespace labels or using Rancher's cluster mapping
	return "local", r.Client
}

// refreshClusterClients periodically refreshes the list of downstream clusters and creates clients
func (r *NamespaceReconciler) refreshClusterClients(ctx context.Context) {
	ticker := time.NewTicker(clusterRefreshInterval)
	defer ticker.Stop()

	// Initial refresh
	r.doRefreshClusterClients(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.doRefreshClusterClients(ctx)
		}
	}
}

func (r *NamespaceReconciler) doRefreshClusterClients(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("refreshing cluster clients")

	// List all clusters from Rancher
	clusterList := &unstructured.UnstructuredList{}
	clusterList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "ClusterList",
	})

	if err := r.List(ctx, clusterList); err != nil {
		logger.Error(err, "unable to list clusters")
		return
	}

	newClusterClients := make(map[string]client.Client)

	// Create clients for each cluster
	for i := range clusterList.Items {
		cluster := &clusterList.Items[i]
		clusterID := cluster.GetName()
		
		// Skip the local cluster (management cluster) - we already have a client for it
		if clusterID == "local" {
			continue
		}

		// Get cluster status to check if it's ready
		status, found, err := unstructured.NestedMap(cluster.Object, "status")
		if err != nil || !found {
			logger.V(1).Info("cluster status not found, skipping", "clusterId", clusterID)
			continue
		}

		// Check if cluster is ready
		conditions, found, _ := unstructured.NestedSlice(status, "conditions")
		if !found {
			logger.V(1).Info("cluster conditions not found, skipping", "clusterId", clusterID)
			continue
		}

		ready := false
		for _, cond := range conditions {
			if condMap, ok := cond.(map[string]interface{}); ok {
				if condType, ok := condMap["type"].(string); ok && condType == "Ready" {
					if condStatus, ok := condMap["status"].(string); ok && condStatus == "True" {
						ready = true
						break
					}
				}
			}
		}

		if !ready {
			logger.V(1).Info("cluster not ready, skipping", "clusterId", clusterID)
			continue
		}

		// Create a client for this cluster using Rancher's cluster proxy
		clusterClient, err := r.createClusterClient(ctx, clusterID)
		if err != nil {
			logger.Error(err, "unable to create client for cluster", "clusterId", clusterID)
			continue
		}

		newClusterClients[clusterID] = clusterClient
		logger.Info("created client for cluster", "clusterId", clusterID)
	}

	// Update cluster clients map
	r.clusterMutex.Lock()
	r.clusterClients = newClusterClients
	r.lastClusterRefresh = time.Now()
	r.clusterMutex.Unlock()

	logger.Info("cluster clients refreshed", "clusterCount", len(newClusterClients))
}

// createClusterClient creates a Kubernetes client for a downstream cluster using Rancher's cluster proxy
func (r *NamespaceReconciler) createClusterClient(ctx context.Context, clusterID string) (client.Client, error) {
	// Get the base REST config from the manager
	config := r.Manager.GetConfig()

	// Create a new config for the cluster proxy
	clusterConfig := rest.CopyConfig(config)
	
	// Rancher's cluster proxy URL format: /k8s/clusters/<cluster-id>
	// We need to modify the API path to include the cluster ID
	// The cluster proxy is accessed through the management cluster's API server
	if clusterConfig.Host != "" {
		// Ensure the host ends with the cluster proxy path
		if !strings.Contains(clusterConfig.Host, "/k8s/clusters/") {
			// Insert cluster proxy path before any existing path
			clusterConfig.Host = strings.TrimSuffix(clusterConfig.Host, "/") + "/k8s/clusters/" + clusterID
		}
	}

	// Create a new client for this cluster
	clusterClient, err := client.New(clusterConfig, client.Options{Scheme: r.Scheme})
	if err != nil {
		return nil, fmt.Errorf("unable to create client for cluster %s: %w", clusterID, err)
	}

	return clusterClient, nil
}

// findProjectByName searches for a Rancher Project by its display name
func (r *NamespaceReconciler) findProjectByName(ctx context.Context, projectName string, clusterID string) (client.Object, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("searching for project", "projectName", projectName, "clusterId", clusterID)

	// List all projects, optionally filtered by cluster
	projectList := &unstructured.UnstructuredList{}
	projectList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "ProjectList",
	})

	var listOptions []client.ListOption
	if clusterID != "" && clusterID != "local" {
		// Filter by cluster namespace if specified
		listOptions = append(listOptions, client.InNamespace(clusterID))
	}

	if err := r.List(ctx, projectList, listOptions...); err != nil {
		logger.V(1).Info("unable to list projects", "error", err)
		return nil, fmt.Errorf("unable to list projects: %w", err)
	}

	// Search through projects for a match by displayName or labels/annotations
	for i := range projectList.Items {
		project := &projectList.Items[i]
		if r.projectMatches(project, projectName) {
			logger.Info("found project by name match", "projectName", projectName, "projectId", project.GetName(), "clusterId", clusterID)
			return project, nil
		}
	}

	return nil, nil
}

// projectMatches checks if a project matches the given name (case-insensitive)
func (r *NamespaceReconciler) projectMatches(project *unstructured.Unstructured, projectName string) bool {
	// First, check spec.displayName (most common location for project display name)
	if displayName, found, err := unstructured.NestedString(project.Object, "spec", "displayName"); err == nil && found {
		if strings.EqualFold(displayName, projectName) {
			return true
		}
	}

	// Check labels
	labels := project.GetLabels()
	for key, value := range labels {
		if strings.EqualFold(value, projectName) {
			return true
		}
		// Also check if key suggests it's a name field
		if (strings.Contains(strings.ToLower(key), "name") ||
			strings.Contains(strings.ToLower(key), "project")) &&
			strings.EqualFold(value, projectName) {
			return true
		}
	}

	// Check annotations
	annotations := project.GetAnnotations()
	for key, value := range annotations {
		if strings.EqualFold(value, projectName) {
			return true
		}
		// Check for display name annotation
		if (strings.Contains(strings.ToLower(key), "name") ||
			strings.Contains(strings.ToLower(key), "display")) &&
			strings.EqualFold(value, projectName) {
			return true
		}
	}

	return false
}

// extractClusterID extracts cluster ID from project ID
// Rancher project IDs are typically in format: c-xxxxx:p-xxxxx
func (r *NamespaceReconciler) extractClusterID(projectID string) string {
	parts := strings.Split(projectID, ":")
	if len(parts) >= 2 {
		return parts[0]
	}
	return ""
}

// updateNamespaceWithProject updates the namespace with project assignment labels and annotations
func (r *NamespaceReconciler) updateNamespaceWithProject(ctx context.Context, namespaceClient client.Client, namespace *corev1.Namespace, projectID, clusterID string) error {
	logger := log.FromContext(ctx)

	// Create a patch for the namespace
	patch := client.MergeFrom(namespace.DeepCopy())

	// Add/update labels
	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}
	namespace.Labels[rancherProjectIDLabel] = projectID
	if clusterID != "" {
		namespace.Labels[rancherClusterIDLabel] = clusterID
	}

	// Add/update annotations
	if namespace.Annotations == nil {
		namespace.Annotations = make(map[string]string)
	}
	namespace.Annotations[rancherProjectIDAnnotation] = projectID

	// Apply the patch using the appropriate cluster client
	if err := namespaceClient.Patch(ctx, namespace, patch); err != nil {
		logger.Error(err, "unable to patch namespace", "namespace", namespace.Name, "clusterId", clusterID)
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Manager = mgr
	r.clusterClients = make(map[string]client.Client)
	r.lastClusterRefresh = time.Time{}

	// Start background goroutine to refresh cluster clients
	ctx := context.Background()
	go r.refreshClusterClients(ctx)

	// Set up controller for management cluster namespaces
	// Note: For downstream clusters, we'll need to access them via Rancher's cluster proxy
	// The reconcile function will determine which cluster a namespace belongs to
	// and use the appropriate client
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{})

	return builder.Complete(r)
}
