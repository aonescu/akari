package authority

import "strings"

type ControllerAuthorityMap struct {
	mappings map[string][]string // field -> []controllers
	metadata map[string]ControllerMetadata
}

type ControllerMetadata struct {
	Name        string
	Description string
	Team        string
	Contact     string
	Priority    int // For conflict resolution
}

func NewControllerAuthorityMap() *ControllerAuthorityMap {
	cam := &ControllerAuthorityMap{
		mappings: make(map[string][]string),
		metadata: make(map[string]ControllerMetadata),
	}
	cam.initializeAuthorities()
	return cam
}

func (cam *ControllerAuthorityMap) initializeAuthorities() {
	// Pod-related authorities
	cam.addAuthority("status.phase", []string{"kubelet"})
	cam.addAuthority("status.conditions", []string{"kubelet"})
	cam.addAuthority("status.containerStatuses", []string{"kubelet"})
	cam.addAuthority("status.initContainerStatuses", []string{"kubelet"})
	cam.addAuthority("status.hostIP", []string{"kubelet"})
	cam.addAuthority("status.podIP", []string{"kubelet"})
	cam.addAuthority("status.startTime", []string{"kubelet"})

	// Scheduling authorities
	cam.addAuthority("spec.nodeName", []string{"kube-scheduler"})

	// Deployment/ReplicaSet authorities
	cam.addAuthority("spec.replicas", []string{"deployment-controller", "replicaset-controller", "statefulset-controller"})
	cam.addAuthority("status.replicas", []string{"replicaset-controller", "deployment-controller", "statefulset-controller"})
	cam.addAuthority("status.readyReplicas", []string{"replicaset-controller", "deployment-controller", "statefulset-controller"})
	cam.addAuthority("status.availableReplicas", []string{"deployment-controller"})
	cam.addAuthority("status.updatedReplicas", []string{"deployment-controller"})

	// Service/Endpoints authorities
	cam.addAuthority("status.loadBalancer", []string{"service-controller", "cloud-controller-manager"})
	cam.addAuthority("status.endpoints", []string{"endpoint-controller", "endpointslice-controller"})

	// Node authorities
	cam.addAuthority("status.conditions", []string{"node-controller", "kubelet"})
	cam.addAuthority("status.allocatable", []string{"kubelet"})
	cam.addAuthority("status.capacity", []string{"kubelet"})
	cam.addAuthority("status.addresses", []string{"kubelet", "cloud-controller-manager"})

	// Volume authorities
	cam.addAuthority("status.phase", []string{"pv-controller", "pvc-protection-controller"})

	// Garbage collection
	cam.addAuthority("metadata.deletionTimestamp", []string{"garbage-collector"})
	cam.addAuthority("metadata.finalizers", []string{"*"}) // Multiple controllers can add finalizers

	// Controller metadata
	cam.metadata["kubelet"] = ControllerMetadata{
		Name:        "kubelet",
		Description: "Node agent that manages pod lifecycle and reports status",
		Team:        "platform-node",
		Contact:     "platform-team@company.com",
		Priority:    1,
	}

	cam.metadata["kube-scheduler"] = ControllerMetadata{
		Name:        "kube-scheduler",
		Description: "Assigns pods to nodes based on resource requirements",
		Team:        "platform",
		Contact:     "platform-team@company.com",
		Priority:    2,
	}

	cam.metadata["deployment-controller"] = ControllerMetadata{
		Name:        "deployment-controller",
		Description: "Manages deployment rollouts and ReplicaSets",
		Team:        "platform",
		Contact:     "platform-team@company.com",
		Priority:    3,
	}

	cam.metadata["replicaset-controller"] = ControllerMetadata{
		Name:        "replicaset-controller",
		Description: "Ensures desired number of pod replicas are running",
		Team:        "platform",
		Contact:     "platform-team@company.com",
		Priority:    3,
	}

	cam.metadata["node-controller"] = ControllerMetadata{
		Name:        "node-controller",
		Description: "Monitors node health and manages node lifecycle",
		Team:        "infrastructure",
		Contact:     "infra-team@company.com",
		Priority:    1,
	}

	cam.metadata["service-controller"] = ControllerMetadata{
		Name:        "service-controller",
		Description: "Manages service endpoints and load balancers",
		Team:        "platform",
		Contact:     "platform-team@company.com",
		Priority:    3,
	}

	cam.metadata["pv-controller"] = ControllerMetadata{
		Name:        "pv-controller",
		Description: "Manages PersistentVolume binding and lifecycle",
		Team:        "storage",
		Contact:     "storage-team@company.com",
		Priority:    4,
	}

	cam.metadata["garbage-collector"] = ControllerMetadata{
		Name:        "garbage-collector",
		Description: "Cleans up orphaned resources",
		Team:        "platform",
		Contact:     "platform-team@company.com",
		Priority:    5,
	}
}

func (cam *ControllerAuthorityMap) addAuthority(field string, controllers []string) {
	cam.mappings[field] = controllers
}

func (cam *ControllerAuthorityMap) GetAuthorizedControllers(field string) []string {
	// Check exact match
	if controllers, exists := cam.mappings[field]; exists {
		return controllers
	}

	// Check prefix match (for nested fields like status.conditions[Ready].status)
	for mappedField, controllers := range cam.mappings {
		if strings.HasPrefix(field, mappedField) {
			return controllers
		}
	}

	return []string{}
}

func (cam *ControllerAuthorityMap) GetAllControllers() []string {
	seen := make(map[string]bool)
	controllers := make([]string, 0)

	for _, ctrls := range cam.mappings {
		for _, ctrl := range ctrls {
			if !seen[ctrl] && ctrl != "*" {
				seen[ctrl] = true
				controllers = append(controllers, ctrl)
			}
		}
	}

	return controllers
}

func (cam *ControllerAuthorityMap) GetControllerMetadata(controller string) (ControllerMetadata, bool) {
	metadata, exists := cam.metadata[controller]
	return metadata, exists
}

func (cam *ControllerAuthorityMap) ValidateAuthority(controller, field string) bool {
	authorized := cam.GetAuthorizedControllers(field)
	for _, ctrl := range authorized {
		if ctrl == controller || ctrl == "*" {
			return true
		}
	}
	return false
}
