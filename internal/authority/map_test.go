package authority

import (
	"testing"
)

func TestControllerAuthorityMap_GetAuthorizedControllers(t *testing.T) {
	cam := NewControllerAuthorityMap()

	// Test exact match
	controllers := cam.GetAuthorizedControllers("spec.nodeName")
	if len(controllers) == 0 {
		t.Fatal("Expected controllers for spec.nodeName")
	}
	if controllers[0] != "kube-scheduler" {
		t.Errorf("Expected 'kube-scheduler', got '%s'", controllers[0])
	}

	// Test prefix match for nested fields
	controllers = cam.GetAuthorizedControllers("status.conditions[Ready].status")
	if len(controllers) == 0 {
		t.Fatal("Expected controllers for status.conditions")
	}
	if !contains(controllers, "kubelet") {
		t.Errorf("Expected 'kubelet' in controllers, got %v", controllers)
	}

	// Test non-existent field
	controllers = cam.GetAuthorizedControllers("non.existent.field")
	if len(controllers) != 0 {
		t.Errorf("Expected empty slice for non-existent field, got %v", controllers)
	}
}

func TestControllerAuthorityMap_GetAllControllers(t *testing.T) {
	cam := NewControllerAuthorityMap()

	controllers := cam.GetAllControllers()
	if len(controllers) == 0 {
		t.Fatal("Expected at least some controllers")
	}

	// Check for some expected controllers
	expectedControllers := []string{"kubelet", "kube-scheduler", "deployment-controller"}
	for _, expected := range expectedControllers {
		found := false
		for _, ctrl := range controllers {
			if ctrl == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected controller '%s' not found in list", expected)
		}
	}
}

func TestControllerAuthorityMap_GetControllerMetadata(t *testing.T) {
	cam := NewControllerAuthorityMap()

	metadata, exists := cam.GetControllerMetadata("kubelet")
	if !exists {
		t.Fatal("Expected metadata for kubelet")
	}

	if metadata.Name != "kubelet" {
		t.Errorf("Expected name 'kubelet', got '%s'", metadata.Name)
	}

	if metadata.Team == "" {
		t.Error("Expected non-empty team")
	}

	// Test non-existent controller
	_, exists = cam.GetControllerMetadata("non-existent-controller")
	if exists {
		t.Error("Expected metadata to not exist for non-existent controller")
	}
}

func TestControllerAuthorityMap_ValidateAuthority(t *testing.T) {
	cam := NewControllerAuthorityMap()

	// Test valid authority - use status.containerStatuses which is uniquely mapped to kubelet
	valid := cam.ValidateAuthority("kubelet", "status.containerStatuses")
	if !valid {
		t.Error("Expected kubelet to have authority over status.containerStatuses")
	}

	// Test invalid authority
	valid = cam.ValidateAuthority("kube-scheduler", "status.containerStatuses")
	if valid {
		t.Error("Expected kube-scheduler to NOT have authority over status.containerStatuses")
	}

	// Test wildcard authority
	valid = cam.ValidateAuthority("any-controller", "metadata.finalizers")
	if !valid {
		t.Error("Expected wildcard authority for metadata.finalizers")
	}
}

func TestControllerAuthorityMap_PrefixMatching(t *testing.T) {
	cam := NewControllerAuthorityMap()

	// Test that prefix matching works for nested fields
	testCases := []struct {
		field      string
		expected   string
		shouldFind bool
	}{
		{"status.conditions[Ready].status", "kubelet", true},
		{"status.conditions[PodScheduled].status", "kubelet", true},
		{"status.containerStatuses[0].state.running", "kubelet", true},
		{"spec.nodeName", "kube-scheduler", true},
		{"metadata.deletionTimestamp", "garbage-collector", true},
		{"unknown.field.path", "", false},
	}

	for _, tc := range testCases {
		controllers := cam.GetAuthorizedControllers(tc.field)
		if tc.shouldFind {
			if len(controllers) == 0 {
				t.Errorf("Expected to find controllers for field '%s'", tc.field)
			} else if tc.expected != "" && !contains(controllers, tc.expected) {
				t.Errorf("Expected '%s' in controllers for field '%s', got %v", tc.expected, tc.field, controllers)
			}
		} else {
			if len(controllers) > 0 {
				t.Errorf("Expected no controllers for field '%s', got %v", tc.field, controllers)
			}
		}
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
