# Controller Architecture Diagram

This document explains the architecture and function call flow of the QN Rancher Operator controller.

## Mermaid Diagram

```mermaid
graph TB
    Start([Application Starts]) --> Main[main function]
    Main --> ParseFlags[Parse Command Flags]
    ParseFlags --> SetLogger[Setup Logger]
    SetLogger --> NewManager[Create Controller Manager]
    NewManager --> SetupController[SetupWithManager]
    SetupController --> RegisterWatch[Register Namespace Watcher]
    RegisterWatch --> StartManager[Start Manager]
    StartManager --> WatchLoop[Watch Loop Active]
    
    WatchLoop -->|Namespace Event| Reconcile[Reconcile Function]
    
    Reconcile --> GetNamespace[Get Namespace from API]
    GetNamespace -->|Not Found| Skip1[Skip - Namespace Deleted]
    GetNamespace -->|Error| Error1[Return Error - Retry]
    GetNamespace -->|Success| CheckAppOwner{Has appOwner<br/>label?}
    
    CheckAppOwner -->|No| Skip2[Skip - No appOwner]
    CheckAppOwner -->|Yes| CheckAssigned{Already<br/>Assigned?}
    
    CheckAssigned -->|Yes| Skip3[Skip - Already Done]
    CheckAssigned -->|No| FindProject[findProjectByName]
    
    FindProject --> TryLabels[Try Label Search]
    TryLabels -->|Found| ReturnProject1[Return Project]
    TryLabels -->|Not Found| ListAll[List All Projects]
    
    ListAll -->|Error| Error2[Return Error - Retry]
    ListAll -->|Success| LoopProjects[Loop Through Projects]
    
    LoopProjects --> ProjectMatch[projectMatches]
    ProjectMatch --> CheckDisplayName{Check<br/>spec.displayName}
    CheckDisplayName -->|Match| ReturnProject2[Return Project]
    CheckDisplayName -->|No Match| CheckLabels{Check<br/>Labels}
    CheckLabels -->|Match| ReturnProject2
    CheckLabels -->|No Match| CheckAnnots{Check<br/>Annotations}
    CheckAnnots -->|Match| ReturnProject2
    CheckAnnots -->|No Match| NextProject[Next Project]
    NextProject -->|More Projects| LoopProjects
    NextProject -->|No More| ReturnNil[Return Nil]
    
    ReturnProject1 --> ExtractID[extractClusterID]
    ReturnProject2 --> ExtractID
    ReturnNil --> Skip4[Skip - Project Not Found]
    
    ExtractID --> UpdateNS[updateNamespaceWithProject]
    UpdateNS --> CreatePatch[Create Merge Patch]
    CreatePatch --> AddLabels[Add Project Labels]
    AddLabels --> AddAnnots[Add Project Annotations]
    AddAnnots --> ApplyPatch[Apply Patch to API]
    ApplyPatch -->|Error| Error3[Return Error - Retry]
    ApplyPatch -->|Success| Success[Success - Namespace Assigned]
    
    style Start fill:#90EE90
    style Success fill:#90EE90
    style Skip1 fill:#FFE4B5
    style Skip2 fill:#FFE4B5
    style Skip3 fill:#FFE4B5
    style Skip4 fill:#FFE4B5
    style Error1 fill:#FFB6C1
    style Error2 fill:#FFB6C1
    style Error3 fill:#FFB6C1
    style Reconcile fill:#87CEEB
    style FindProject fill:#87CEEB
    style UpdateNS fill:#87CEEB
```

## Function Call Sequence Diagram

```mermaid
sequenceDiagram
    participant K8s as Kubernetes API
    participant CM as Controller Manager
    participant NR as NamespaceReconciler
    participant Rancher as Rancher API
    
    Note over K8s: Namespace created/updated<br/>with appOwner label
    
    K8s->>CM: Namespace Event
    CM->>NR: Reconcile(ctx, req)
    
    NR->>K8s: Get(namespace)
    K8s-->>NR: Namespace object
    
    Note over NR: Check appOwner label<br/>Check if already assigned
    
    NR->>NR: findProjectByName(appOwner)
    
    alt Label Search
        NR->>Rancher: List Projects (with label selector)
        Rancher-->>NR: Project List
    else Full List Search
        NR->>Rancher: List All Projects
        Rancher-->>NR: All Projects
        loop For each project
            NR->>NR: projectMatches(project, name)
            Note over NR: Check spec.displayName<br/>Check labels<br/>Check annotations
        end
    end
    
    NR->>NR: extractClusterID(projectID)
    NR->>NR: updateNamespaceWithProject()
    NR->>K8s: Patch(namespace, labels+annotations)
    K8s-->>NR: Success
    
    Note over K8s: Namespace now assigned<br/>to Rancher Project
```

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                      │
│                                                                  │
│  ┌──────────────────┐         ┌──────────────────────────┐   │
│  │   Namespaces     │◄────────┤  Controller Manager       │   │
│  │  (with appOwner  │         │  (controller-runtime)     │   │
│  │     labels)      │         │                           │   │
│  └──────────────────┘         └──────────────────────────┘   │
│           ▲                              │                      │
│           │                              │                      │
│           │                              ▼                      │
│           │                    ┌──────────────────────────┐   │
│           │                    │  NamespaceReconciler     │   │
│           │                    │  (Our Controller)        │   │
│           │                    └──────────────────────────┘   │
│           │                              │                      │
│           │                              │                      │
│           └──────────────────────────────┘                      │
│                                                                  │
│  ┌──────────────────┐         ┌──────────────────────────┐   │
│  │ Rancher Projects │◄────────┤  Rancher Management API   │   │
│  │ (management.cattle│         │  (management.cattle.io)   │   │
│  │      .io/v3)      │         │                           │   │
│  └──────────────────┘         └──────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Function Call Flow

### 1. Application Startup (main.go)

```
main()
  │
  ├─► Parse command-line flags (metrics, health probe, leader election)
  │
  ├─► ctrl.SetLogger() - Setup structured logging
  │
  ├─► ctrl.NewManager() - Create controller manager
  │   │
  │   ├─► Initialize Kubernetes client
  │   ├─► Setup informers/cache
  │   ├─► Setup metrics server
  │   └─► Setup health probes
  │
  ├─► NamespaceReconciler.SetupWithManager()
  │   │
  │   └─► ctrl.NewControllerManagedBy()
  │       │
  │       ├─► Register watch for core/v1 Namespace resources
  │       └─► Register Reconcile() as the handler
  │
  ├─► mgr.AddHealthzCheck() - Add health check endpoint
  ├─► mgr.AddReadyzCheck() - Add readiness check endpoint
  │
  └─► mgr.Start() - Start the controller manager
      │
      └─► Begin watching namespaces and triggering Reconcile()
```

### 2. Reconciliation Loop (controllers/namespace_controller.go)

```
Controller Manager detects Namespace event
  │
  ▼
Reconcile(ctx, req ctrl.Request)
  │
  ├─► r.Get(ctx, req.NamespacedName, namespace)
  │   └─► Fetch Namespace from Kubernetes API
  │       │
  │       ├─► If NotFound → return (namespace deleted)
  │       └─► If error → return error
  │
  ├─► Check namespace.Labels["appOwner"]
  │   │
  │   ├─► If missing/empty → return (skip)
  │   └─► If exists → continue
  │
  ├─► Check namespace.Labels["field.cattle.io/projectId"]
  │   │
  │   ├─► If already assigned → return (skip, already done)
  │   └─► If not assigned → continue
  │
  ├─► r.findProjectByName(ctx, appOwner)
  │   │
  │   ├─► Try label-based search (efficient)
  │   │   │
  │   │   ├─► Search with label "project.cattle.io/name"
  │   │   ├─► Search with label "cattle.io/projectName"
  │   │   └─► Search with label "field.cattle.io/projectName"
  │   │
  │   ├─► If label search fails → List all projects
  │   │   │
  │   │   └─► r.List(ctx, projectList)
  │   │       │
  │   │       └─► For each project:
  │   │           │
  │   │           └─► r.projectMatches(project, projectName)
  │   │               │
  │   │               ├─► Check spec.displayName
  │   │               ├─► Check labels (case-insensitive)
  │   │               └─► Check annotations (case-insensitive)
  │   │
  │   └─► Return matching project or nil
  │
  ├─► If project == nil → return (project not found)
  │
  ├─► project.GetName() - Get project ID (format: c-xxxxx:p-xxxxx)
  │
  ├─► r.extractClusterID(projectID)
  │   │
  │   └─► Split projectID by ":" and return first part (cluster ID)
  │
  └─► r.updateNamespaceWithProject(ctx, namespace, projectID, clusterID)
      │
      ├─► Create patch using client.MergeFrom()
      │
      ├─► Add labels:
      │   ├─► field.cattle.io/projectId = projectID
      │   └─► field.cattle.io/clusterId = clusterID (if available)
      │
      ├─► Add annotations:
      │   └─► field.cattle.io/projectId = projectID
      │
      └─► r.Patch(ctx, namespace, patch)
          │
          └─► Apply patch to Kubernetes API
              │
              └─► Namespace is now assigned to Rancher Project ✓
```

## Detailed Function Relationships

### Setup Phase

```
main()
  │
  ├─► init()
  │   └─► clientgoscheme.AddToScheme() - Register Kubernetes core types
  │
  ├─► ctrl.NewManager()
  │   └─► Creates Manager with:
  │       ├─► Client (for API calls)
  │       ├─► Cache (for watching resources)
  │       ├─► Scheme (type registry)
  │       └─► Event recorder
  │
  └─► NamespaceReconciler.SetupWithManager()
      │
      └─► ctrl.NewControllerManagedBy(mgr)
          │
          ├─► For(&corev1.Namespace{}) - Watch Namespace resources
          │
          └─► Complete(r) - Register Reconcile() as handler
```

### Runtime Phase (Event-Driven)

```
Kubernetes Event (Namespace created/updated)
  │
  ▼
Controller Manager
  │
  ├─► Detects event via informer
  │
  ├─► Creates ctrl.Request{Name: namespace.Name}
  │
  └─► Calls Reconcile(ctx, req)
      │
      └─► [See Reconciliation Loop above]
```

## Data Flow

```
┌──────────────┐
│  Namespace   │
│              │
│ Labels:      │
│  appOwner:   │──┐
│   "DevOps"   │  │
└──────────────┘  │
                  │
                  ▼
         ┌──────────────────────┐
         │    Reconcile()       │
         └──────────────────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │  findProjectByName   │
         │    (appOwner)        │
         └──────────────────────┘
                  │
        ┌─────────┴─────────┐
        │                   │
        ▼                   ▼
┌──────────────┐   ┌─────────────────┐
│ Label Search │   │ List All + Match│
│ (efficient)  │   │ (fallback)      │
└──────────────┘   └─────────────────┘
        │                   │
        └─────────┬─────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │  projectMatches()    │
         │  - spec.displayName   │
         │  - labels             │
         │  - annotations        │
         └──────────────────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │   Project Found      │
         │   ID: c-xxx:p-xxx    │
         └──────────────────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │  extractClusterID    │
         │  (from projectID)    │
         └──────────────────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │updateNamespaceWith   │
         │      Project()        │
         │                       │
         │ Adds:                 │
         │  - projectId label    │
         │  - clusterId label    │
         │  - projectId annot    │
         └──────────────────────┘
                  │
                  ▼
         ┌──────────────────────┐
         │   Namespace           │
         │   Updated ✓          │
         └──────────────────────┘
```

## Key Components

### NamespaceReconciler Struct

```go
type NamespaceReconciler struct {
    client.Client  // Kubernetes client for API calls
    Scheme *runtime.Scheme  // Type registry
}
```

### Main Functions

1. **Reconcile()** - Main reconciliation logic
   - Entry point for each namespace event
   - Orchestrates the assignment process

2. **findProjectByName()** - Project discovery
   - Searches for Rancher Projects by name
   - Uses multiple strategies (labels, then full list)

3. **projectMatches()** - Project matching logic
   - Checks spec.displayName
   - Checks labels and annotations
   - Case-insensitive matching

4. **extractClusterID()** - ID parsing
   - Extracts cluster ID from project ID format

5. **updateNamespaceWithProject()** - Namespace update
   - Adds required labels and annotations
   - Uses strategic merge patch

6. **SetupWithManager()** - Controller registration
   - Registers the controller with the manager
   - Sets up namespace watching

## Event Flow Example

```
1. User creates namespace:
   kubectl create namespace my-app
   kubectl label namespace my-app appOwner=DevOps

2. Kubernetes API Server emits event

3. Controller Manager's informer detects event

4. Manager queues Reconcile request:
   Reconcile(ctx, {Name: "my-app"})

5. Reconcile() executes:
   - Fetches namespace "my-app"
   - Finds appOwner="DevOps"
   - Searches for "DevOps" project
   - Finds project "c-abc123:p-xyz789"
   - Updates namespace with project labels

6. Namespace now has:
   labels:
     appOwner: DevOps
     field.cattle.io/projectId: c-abc123:p-xyz789
     field.cattle.io/clusterId: c-abc123
   annotations:
     field.cattle.io/projectId: c-abc123:p-xyz789

7. Rancher recognizes namespace as part of "DevOps" project ✓
```

## Error Handling

```
Reconcile()
  │
  ├─► Namespace not found → return (no error, just skip)
  │
  ├─► No appOwner label → return (no error, skip)
  │
  ├─► Already assigned → return (no error, skip)
  │
  ├─► Project not found → return (no error, log and skip)
  │
  ├─► Error fetching namespace → return error (retry)
  │
  ├─► Error listing projects → return error (retry)
  │
  └─► Error patching namespace → return error (retry)
```

Controller-runtime automatically retries on errors with exponential backoff.
