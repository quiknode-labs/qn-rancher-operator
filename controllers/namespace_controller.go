package controllers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=management.cattle.io,resources=projects,verbs=get;list;watch;create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Namespace instance
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, namespace); err != nil {
		if errors.IsNotFound(err) {
			// Namespace was deleted, nothing to do
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Namespace")
		return ctrl.Result{}, err
	}

	// Check if namespace has appOwner label
	appOwner, exists := namespace.Labels[appOwnerLabel]
	if !exists || appOwner == "" {
		logger.V(1).Info("namespace does not have appOwner label, skipping", "namespace", namespace.Name)
		return ctrl.Result{}, nil
	}

	// Check if namespace is already assigned to a project
	if projectID, hasProject := namespace.Labels[rancherProjectIDLabel]; hasProject && projectID != "" {
		logger.V(1).Info("namespace already assigned to project", "namespace", namespace.Name, "projectId", projectID)
		return ctrl.Result{}, nil
	}

	logger.Info("processing namespace with appOwner label", "namespace", namespace.Name, "appOwner", appOwner)

	// Find the Rancher Project by name (case-insensitive)
	project, err := r.findProjectByName(ctx, appOwner)
	if err != nil {
		logger.Error(err, "unable to find project", "projectName", appOwner)
		return ctrl.Result{}, err
	}

	// If project doesn't exist, create it
	if project == nil {
		logger.Info("project not found, creating new project", "projectName", appOwner, "namespace", namespace.Name)
		project, err = r.createProject(ctx, appOwner)
		if err != nil {
			logger.Error(err, "unable to create project", "projectName", appOwner)
			return ctrl.Result{}, err
		}
		logger.Info("successfully created project", "projectName", appOwner, "projectId", project.GetName())
	}

	// Get project ID and cluster ID from the project
	projectID := project.GetName()
	clusterID := r.extractClusterID(projectID)

	if projectID == "" {
		logger.Info("project ID is empty, skipping", "projectName", appOwner)
		return ctrl.Result{}, nil
	}

	// Update namespace with project labels and annotations
	if err := r.updateNamespaceWithProject(ctx, namespace, projectID, clusterID); err != nil {
		logger.Error(err, "unable to update namespace with project assignment", "namespace", namespace.Name)
		return ctrl.Result{}, err
	}

	logger.Info("successfully assigned namespace to project", "namespace", namespace.Name, "projectId", projectID, "clusterId", clusterID)
	return ctrl.Result{}, nil
}

// findProjectByName searches for a Rancher Project by its display name
func (r *NamespaceReconciler) findProjectByName(ctx context.Context, projectName string) (client.Object, error) {
	logger := log.FromContext(ctx)

	// Rancher Projects are stored as custom resources in the management.cattle.io/v3 API
	// Projects are typically named in the format: <cluster-id>:<project-id>
	// The display name is stored in spec.displayName

	logger.V(1).Info("searching for project", "projectName", projectName)

	// Note: Label-based search is case-sensitive in Kubernetes, so we'll rely on
	// the full list search with case-insensitive matching instead

	// If label search fails, list all projects and match by displayName
	projectList := &unstructured.UnstructuredList{}
	projectList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "ProjectList",
	})

	if err := r.List(ctx, projectList); err != nil {
		logger.V(1).Info("unable to list projects, may need to configure Rancher API access", "error", err)
		return nil, fmt.Errorf("unable to list projects: %w", err)
	}

	// Search through projects for a match by displayName or labels/annotations
	for i := range projectList.Items {
		project := &projectList.Items[i]
		if r.projectMatches(project, projectName) {
			logger.Info("found project by name match", "projectName", projectName, "projectId", project.GetName())
			return project, nil
		}
	}

	return nil, nil
}

// projectMatches checks if a project matches the given name
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

// sanitizeProjectID converts a project display name to a valid project ID
// Rancher project IDs are typically in format: p-xxxxx
func (r *NamespaceReconciler) sanitizeProjectID(projectName string) string {
	// Convert to lowercase
	id := strings.ToLower(projectName)

	// Replace spaces and common separators with hyphens
	id = strings.ReplaceAll(id, " ", "-")
	id = strings.ReplaceAll(id, "_", "-")
	id = strings.ReplaceAll(id, ".", "-")

	// Remove any characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile("[^a-z0-9-]")
	id = reg.ReplaceAllString(id, "")

	// Remove consecutive hyphens
	reg = regexp.MustCompile("-+")
	id = reg.ReplaceAllString(id, "-")

	// Remove leading/trailing hyphens
	id = strings.Trim(id, "-")

	// Ensure it starts with "p-"
	if !strings.HasPrefix(id, "p-") {
		id = "p-" + id
	}

	// Limit length (Kubernetes names have a 253 char limit, but project IDs are typically shorter)
	if len(id) > 63 {
		id = id[:63]
		id = strings.TrimSuffix(id, "-")
	}

	return id
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

// createProject creates a new Rancher Project with the given display name
func (r *NamespaceReconciler) createProject(ctx context.Context, projectName string) (client.Object, error) {
	logger := log.FromContext(ctx)

	// First, we need to get the cluster ID
	// Try to get it from an existing project or namespace
	clusterID, err := r.getClusterID(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to determine cluster ID: %w", err)
	}

	if clusterID == "" {
		return nil, fmt.Errorf("cluster ID is empty, cannot create project")
	}

	// Create a new Project resource
	project := &unstructured.Unstructured{}
	project.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "Project",
	})

	// Rancher projects are named as <cluster-id>:<project-id>
	// Generate a project ID from the project name (sanitized for Kubernetes naming)
	projectID := r.sanitizeProjectID(projectName)
	projectNameFull := clusterID + ":" + projectID

	// Check if a project with this name already exists (might have been created by another process)
	existingProject := &unstructured.Unstructured{}
	existingProject.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "Project",
	})

	if err := r.Get(ctx, client.ObjectKey{Name: projectNameFull}, existingProject); err == nil {
		// Project already exists, return it
		logger.Info("project already exists with generated ID", "projectName", projectName, "projectId", projectNameFull)
		return existingProject, nil
	} else if !errors.IsNotFound(err) {
		// Some other error occurred
		return nil, fmt.Errorf("unable to check for existing project: %w", err)
	}

	// Project doesn't exist, create it
	project.SetName(projectNameFull)

	// Set labels
	project.SetLabels(map[string]string{
		"field.cattle.io/projectName": projectName,
	})

	// Set annotations
	project.SetAnnotations(map[string]string{
		"field.cattle.io/projectName": projectName,
	})

	// Set the spec with displayName
	if err := unstructured.SetNestedField(project.Object, projectName, "spec", "displayName"); err != nil {
		return nil, fmt.Errorf("unable to set displayName: %w", err)
	}

	// Set clusterName in spec (this tells Rancher which cluster this project belongs to)
	if err := unstructured.SetNestedField(project.Object, clusterID, "spec", "clusterName"); err != nil {
		return nil, fmt.Errorf("unable to set clusterName: %w", err)
	}

	// Create the project
	if err := r.Create(ctx, project); err != nil {
		logger.Error(err, "unable to create project", "projectName", projectName, "clusterId", clusterID)
		return nil, fmt.Errorf("unable to create project: %w", err)
	}

	logger.Info("created new project", "projectName", projectName, "projectId", project.GetName(), "clusterId", clusterID)
	return project, nil
}

// getClusterID attempts to determine the cluster ID from existing resources
func (r *NamespaceReconciler) getClusterID(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx)

	// Try to get cluster ID from an existing project
	projectList := &unstructured.UnstructuredList{}
	projectList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "management.cattle.io",
		Version: "v3",
		Kind:    "ProjectList",
	})

	if err := r.List(ctx, projectList, client.Limit(1)); err == nil && len(projectList.Items) > 0 {
		// Extract cluster ID from existing project namespace (projects are namespaced, namespace = cluster ID)
		// Projects are stored in namespaces like "c-xxxxx" where the namespace IS the cluster ID
		clusterID := projectList.Items[0].GetNamespace()
		if clusterID != "" {
			logger.V(1).Info("found cluster ID from existing project namespace", "clusterId", clusterID)
			return clusterID, nil
		}
		// Fallback: try to extract from project name (format: c-xxxxx:p-xxxxx) if namespace is empty
		projectID := projectList.Items[0].GetName()
		clusterID = r.extractClusterID(projectID)
		if clusterID != "" {
			logger.V(1).Info("found cluster ID from existing project name", "clusterId", clusterID)
			return clusterID, nil
		}
	}

	// Try to get cluster ID from a namespace with project assignment
	namespaceList := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaceList, client.Limit(10)); err == nil {
		for _, ns := range namespaceList.Items {
			if clusterID, ok := ns.Labels[rancherClusterIDLabel]; ok && clusterID != "" {
				logger.V(1).Info("found cluster ID from namespace", "clusterId", clusterID)
				return clusterID, nil
			}
		}
	}

	// Try to get cluster ID from the current namespace (if controller is in a namespace with cluster label)
	// This is a fallback - in practice, you might want to configure this via environment variable or config
	return "", fmt.Errorf("unable to determine cluster ID from existing resources")
}

// updateNamespaceWithProject updates the namespace with project assignment labels and annotations
func (r *NamespaceReconciler) updateNamespaceWithProject(ctx context.Context, namespace *corev1.Namespace, projectID, clusterID string) error {
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

	// Apply the patch
	if err := r.Patch(ctx, namespace, patch); err != nil {
		logger.Error(err, "unable to patch namespace", "namespace", namespace.Name)
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
