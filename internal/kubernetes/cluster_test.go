package k8s_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/u2takey/go-utils/filesystem/homedir"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aonescu/akari/internal/dsl"
	"github.com/aonescu/akari/internal/engine"
	"github.com/aonescu/akari/internal/formatting"
	"github.com/aonescu/akari/internal/state"
	"github.com/aonescu/akari/internal/types"
)

type ProductionClusterTest struct {
	clientset *kubernetes.Clientset
	store     state.StateStore
	engine    *engine.InvariantEngine
	results   *TestResults
}

type TestResults struct {
	StartTime       time.Time
	EndTime         time.Time
	TotalResources  int
	ViolationsFound int
	ByKind          map[string]int
	BySeverity      map[string]int
	ByActor         map[string]int
	CriticalIssues  []*engine.ViolationResult
	TopInvariants   []string
}

func NewProductionClusterTest(store state.StateStore, engine *engine.InvariantEngine) (*ProductionClusterTest, error) {
	var kubeconfig string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &ProductionClusterTest{
		clientset: clientset,
		store:     store,
		engine:    engine,
		results: &TestResults{
			ByKind:     make(map[string]int),
			BySeverity: make(map[string]int),
			ByActor:    make(map[string]int),
		},
	}, nil
}

func (pct *ProductionClusterTest) Run(ctx context.Context) error {
	fmt.Println("\n" + strings.Repeat("═", 100))
	fmt.Println("PRODUCTION CLUSTER TEST - LIVE KUBERNETES CLUSTER ANALYSIS")
	fmt.Println(strings.Repeat("═", 100) + "\n")

	pct.results.StartTime = time.Now()

	// Step 1: Discover cluster
	fmt.Println("📡 STEP 1: Discovering cluster resources...")
	if err := pct.discoverCluster(ctx); err != nil {
		return fmt.Errorf("failed to discover cluster: %w", err)
	}

	// Step 2: Analyze cluster health
	fmt.Println("\n🔍 STEP 2: Analyzing cluster health...")
	if err := pct.analyzeHealth(); err != nil {
		return fmt.Errorf("failed to analyze health: %w", err)
	}

	// Step 3: Generate report
	fmt.Println("\n📊 STEP 3: Generating analysis report...")
	pct.generateReport()

	pct.results.EndTime = time.Now()

	fmt.Println("\n✅ Production cluster test completed")
	fmt.Printf("Duration: %v\n", pct.results.EndTime.Sub(pct.results.StartTime))

	return nil
}

func (pct *ProductionClusterTest) discoverCluster(ctx context.Context) error {
	// Get all nodes
	nodes, err := pct.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	fmt.Printf("   Found %d nodes\n", len(nodes.Items))
	for _, node := range nodes.Items {
		event := pct.nodeToStateEvent(&node)
		pct.store.Record(event)
		pct.results.TotalResources++
		pct.results.ByKind["Node"]++
	}

	// Get all pods across all namespaces
	pods, err := pct.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	fmt.Printf("   Found %d pods\n", len(pods.Items))
	for _, pod := range pods.Items {
		event := pct.podToStateEvent(&pod)
		pct.store.Record(event)
		pct.results.TotalResources++
		pct.results.ByKind["Pod"]++
	}

	// Get all services
	services, err := pct.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	fmt.Printf("   Found %d services\n", len(services.Items))
	for _, svc := range services.Items {
		event := pct.serviceToStateEvent(&svc)
		pct.store.Record(event)
		pct.results.TotalResources++
		pct.results.ByKind["Service"]++
	}

	return nil
}

func (pct *ProductionClusterTest) analyzeHealth() error {
	violations := pct.engine.EvaluateAll()

	for _, v := range violations {
		if v.Violated {
			pct.results.ViolationsFound++
			pct.results.BySeverity[string(v.Severity)]++
			pct.results.ByActor[v.ResponsibleActor]++

			if v.Severity == dsl.Critical {
				pct.results.CriticalIssues = append(pct.results.CriticalIssues, v)
			}
		}
	}

	return nil
}

func (pct *ProductionClusterTest) generateReport() {
	fmt.Println("\n╔═══════════════════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                              CLUSTER HEALTH REPORT                                            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════════════════════════════╝")

	// Overall health
	healthScore := 100.0
	if pct.results.TotalResources > 0 {
		healthScore = (1.0 - float64(pct.results.ViolationsFound)/float64(pct.results.TotalResources)) * 100
	}

	fmt.Printf("\n🎯 OVERALL HEALTH SCORE: %.1f%%\n", healthScore)
	fmt.Printf("   Total Resources: %d\n", pct.results.TotalResources)
	fmt.Printf("   Violations Found: %d\n", pct.results.ViolationsFound)

	// By resource kind
	fmt.Println("\n📦 RESOURCES BY KIND:")
	for kind, count := range pct.results.ByKind {
		fmt.Printf("   %-20s %d\n", kind+":", count)
	}

	// By severity
	if len(pct.results.BySeverity) > 0 {
		fmt.Println("\n🚨 VIOLATIONS BY SEVERITY:")
		if count, ok := pct.results.BySeverity["critical"]; ok && count > 0 {
			fmt.Printf("   🔴 Critical:  %d\n", count)
		}
		if count, ok := pct.results.BySeverity["degraded"]; ok && count > 0 {
			fmt.Printf("   🟡 Degraded:  %d\n", count)
		}
		if count, ok := pct.results.BySeverity["warning"]; ok && count > 0 {
			fmt.Printf("   🟢 Warning:   %d\n", count)
		}
	}

	// By responsible actor
	if len(pct.results.ByActor) > 0 {
		fmt.Println("\n👤 VIOLATIONS BY RESPONSIBLE ACTOR:")
		for actor, count := range pct.results.ByActor {
			fmt.Printf("   %-30s %d violations\n", actor+":", count)
		}
	}

	// Critical issues detail
	if len(pct.results.CriticalIssues) > 0 {
		fmt.Printf("\n🔴 CRITICAL ISSUES REQUIRING IMMEDIATE ATTENTION (%d):\n", len(pct.results.CriticalIssues))
		fmt.Println(strings.Repeat("-", 100))

		for i, issue := range pct.results.CriticalIssues {
			if i >= 5 {
				fmt.Printf("\n   ... and %d more critical issues (run full analysis for details)\n",
					len(pct.results.CriticalIssues)-5)
				break
			}

			fmt.Printf("\n   [ISSUE %d] %s\n", i+1, issue.InvariantID)
			fmt.Printf("   Resource: %s\n", issue.AffectedResource)
			fmt.Printf("   Reason: %s\n", issue.Reason)
			fmt.Printf("   Responsible: %s\n", issue.ResponsibleActor)
			if len(issue.EliminatedActors) > 0 {
				fmt.Printf("   Eliminated: %v\n", issue.EliminatedActors)
			}
		}
	} else {
		fmt.Println("\n✅ NO CRITICAL ISSUES - Cluster is healthy!")
	}

	// Recommendations
	fmt.Println("\n💡 RECOMMENDATIONS:")
	if pct.results.BySeverity["critical"] > 0 {
		fmt.Println("   1. Address critical issues immediately")
		fmt.Println("   2. Review actor responsibilities in authority map")
		fmt.Println("   3. Set up monitoring for these invariants")
	} else {
		fmt.Println("   1. Cluster appears healthy - continue monitoring")
		fmt.Println("   2. Consider enabling continuous monitoring")
		fmt.Println("   3. Review degraded/warning issues during next maintenance window")
	}

	fmt.Println("\n" + strings.Repeat("═", 100))
}

func (pct *ProductionClusterTest) nodeToStateEvent(node *corev1.Node) types.StateEvent {
	event := types.StateEvent{
		UID:       string(node.UID),
		Kind:      "Node",
		Name:      node.Name,
		Version:   node.ResourceVersion,
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     "node-controller",
		FullState: node,
	}

	for _, cond := range node.Status.Conditions {
		event.FieldDiff[fmt.Sprintf("status.conditions[%s].status", cond.Type)] = string(cond.Status)
	}

	return event
}

func (pct *ProductionClusterTest) podToStateEvent(pod *corev1.Pod) types.StateEvent {
	event := types.StateEvent{
		UID:       string(pod.UID),
		Kind:      "Pod",
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Version:   pod.ResourceVersion,
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     fmt.Sprintf("kubelet/%s", pod.Spec.NodeName),
		FullState: pod,
	}

	if pod.Spec.NodeName != "" {
		event.FieldDiff["spec.nodeName"] = pod.Spec.NodeName
	}

	event.FieldDiff["status.phase"] = string(pod.Status.Phase)

	for _, cond := range pod.Status.Conditions {
		event.FieldDiff[fmt.Sprintf("status.conditions[%s].status", cond.Type)] = string(cond.Status)
		if cond.Reason != "" {
			event.FieldDiff[fmt.Sprintf("status.conditions[%s].reason", cond.Type)] = cond.Reason
		}
	}

	runningCount := 0
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running != nil {
			runningCount++
		}
		if cs.State.Waiting != nil {
			event.FieldDiff["status.containerStatuses.waiting.reason"] = cs.State.Waiting.Reason
		}
	}
	event.FieldDiff["status.containerStatuses.running"] = runningCount

	return event
}

func (pct *ProductionClusterTest) serviceToStateEvent(svc *corev1.Service) types.StateEvent {
	event := types.StateEvent{
		UID:       string(svc.UID),
		Kind:      "Service",
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Version:   svc.ResourceVersion,
		Timestamp: time.Now(),
		FieldDiff: make(map[string]interface{}),
		Actor:     "service-controller",
		FullState: svc,
	}

	if len(svc.Spec.Selector) > 0 {
		event.FieldDiff["spec.selector"] = svc.Spec.Selector
	}

	return event
}

type MockKubernetesCluster struct {
	Nodes       map[string]*MockNode
	Pods        map[string]*MockPod
	Services    map[string]*MockService
	Deployments map[string]*MockDeployment
}

type MockNode struct {
	Name       string
	Ready      bool
	Conditions map[string]string
	Capacity   map[string]string
}

type MockPod struct {
	UID             string
	Name            string
	Namespace       string
	NodeName        string
	Phase           string
	Conditions      map[string]MockCondition
	ContainerStatus MockContainerStatus
	Labels          map[string]string
}

type MockCondition struct {
	Status string
	Reason string
}

type MockContainerStatus struct {
	Running       bool
	WaitingReason string
	RestartCount  int
}

type MockService struct {
	Name      string
	Namespace string
	Selector  map[string]string
	Endpoints int
}

type MockDeployment struct {
	Name              string
	Namespace         string
	Replicas          int
	AvailableReplicas int
}

func NewMockCluster() *MockKubernetesCluster {
	return &MockKubernetesCluster{
		Nodes:       make(map[string]*MockNode),
		Pods:        make(map[string]*MockPod),
		Services:    make(map[string]*MockService),
		Deployments: make(map[string]*MockDeployment),
	}
}

func (c *MockKubernetesCluster) AddNode(name string, ready bool) *MockNode {
	node := &MockNode{
		Name:  name,
		Ready: ready,
		Conditions: map[string]string{
			"Ready":              "True",
			"MemoryPressure":     "False",
			"DiskPressure":       "False",
			"PIDPressure":        "False",
			"NetworkUnavailable": "False",
		},
		Capacity: map[string]string{
			"cpu":    "4",
			"memory": "16Gi",
		},
	}
	if !ready {
		node.Conditions["Ready"] = "False"
	}
	c.Nodes[name] = node
	return node
}

func (c *MockKubernetesCluster) AddPod(name, namespace, nodeName string) *MockPod {
	pod := &MockPod{
		UID:       fmt.Sprintf("pod-%s-%d", name, time.Now().UnixNano()),
		Name:      name,
		Namespace: namespace,
		NodeName:  nodeName,
		Phase:     "Running",
		Conditions: map[string]MockCondition{
			"Ready":        {Status: "True", Reason: ""},
			"PodScheduled": {Status: "True", Reason: ""},
			"Initialized":  {Status: "True", Reason: ""},
		},
		ContainerStatus: MockContainerStatus{
			Running:      true,
			RestartCount: 0,
		},
		Labels: make(map[string]string),
	}
	c.Pods[pod.UID] = pod
	return pod
}

func (c *MockKubernetesCluster) AddService(name, namespace string, selector map[string]string) *MockService {
	svc := &MockService{
		Name:      name,
		Namespace: namespace,
		Selector:  selector,
		Endpoints: 0,
	}
	c.Services[name] = svc
	return svc
}

func (c *MockKubernetesCluster) ToStateEvents() []types.StateEvent {
	events := make([]types.StateEvent, 0)

	// Convert nodes to events
	for _, node := range c.Nodes {
		event := types.StateEvent{
			UID:       fmt.Sprintf("node-%s", node.Name),
			Kind:      "Node",
			Name:      node.Name,
			Version:   fmt.Sprintf("%d", time.Now().Unix()),
			Timestamp: time.Now(),
			FieldDiff: make(map[string]interface{}),
			Actor:     "node-controller",
		}

		for condType, status := range node.Conditions {
			event.FieldDiff[fmt.Sprintf("status.conditions[%s].status", condType)] = status
		}

		events = append(events, event)
	}

	// Convert pods to events
	for _, pod := range c.Pods {
		event := types.StateEvent{
			UID:       pod.UID,
			Kind:      "Pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Version:   fmt.Sprintf("%d", time.Now().Unix()),
			Timestamp: time.Now(),
			FieldDiff: make(map[string]interface{}),
			Actor:     fmt.Sprintf("kubelet/%s", pod.NodeName),
		}

		if pod.NodeName != "" {
			event.FieldDiff["spec.nodeName"] = pod.NodeName
		}

		event.FieldDiff["status.phase"] = pod.Phase

		for condType, cond := range pod.Conditions {
			event.FieldDiff[fmt.Sprintf("status.conditions[%s].status", condType)] = cond.Status
			if cond.Reason != "" {
				event.FieldDiff[fmt.Sprintf("status.conditions[%s].reason", condType)] = cond.Reason
			}
		}

		if pod.ContainerStatus.Running {
			event.FieldDiff["status.containerStatuses.running"] = 1
		} else {
			event.FieldDiff["status.containerStatuses.running"] = 0
			if pod.ContainerStatus.WaitingReason != "" {
				event.FieldDiff["status.containerStatuses.waiting.reason"] = pod.ContainerStatus.WaitingReason
			}
		}

		events = append(events, event)
	}

	// Convert services to events
	for _, svc := range c.Services {
		event := types.StateEvent{
			UID:       fmt.Sprintf("svc-%s-%s", svc.Namespace, svc.Name),
			Kind:      "Service",
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Version:   fmt.Sprintf("%d", time.Now().Unix()),
			Timestamp: time.Now(),
			FieldDiff: make(map[string]interface{}),
			Actor:     "service-controller",
		}

		event.FieldDiff["spec.selector"] = svc.Selector
		if svc.Endpoints > 0 {
			event.FieldDiff["status.endpoints"] = svc.Endpoints
		}

		events = append(events, event)
	}

	return events
}

type TestScenario struct {
	Name        string
	Description string
	Setup       func(*MockKubernetesCluster)
	Expected    ExpectedResults
	Explanation string
	Events      []types.StateEvent
}

type ExpectedResults struct {
	ViolationCount  int
	CriticalCount   int
	PrimaryActors   []string
	RootInvariantID string
	ShouldHaveChain bool
}

// ============================================================================
// COMPREHENSIVE TEST SCENARIOS (Real-World Kubernetes Failures)
// ============================================================================

func GetTestScenarios() []TestScenario {
	return []TestScenario{
		// ================================================================
		// SCENARIO 1: Image Pull Failure (Most Common Production Issue)
		// ================================================================
		{
			Name:        "Image Pull Failure - Private Registry Authentication",
			Description: "Pod cannot pull image from private registry due to missing or invalid credentials",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("api-backend", "production", "node-1")
				pod.Phase = "Pending"
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: "ContainersNotReady"}
				pod.ContainerStatus.Running = false
				pod.ContainerStatus.WaitingReason = "ImagePullBackOff"
				pod.Labels["app"] = "api"

				cluster.AddService("api-service", "production", map[string]string{"app": "api"})
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   3,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "image_pulled",
				ShouldHaveChain: true,
			},
			Explanation: `
ROOT CAUSE: Container image cannot be pulled from registry
REASON: ImagePullBackOff indicates authentication failure or image not found
IMPACT: Pod cannot start → Service has no endpoints → Application is down
RESPONSIBILITY: kubelet (executor) but likely configuration issue (imagePullSecrets)
FIX: Verify image name, check imagePullSecrets, ensure registry credentials are correct`,
		},

		// ================================================================
		// SCENARIO 2: Resource Exhaustion - Cannot Schedule
		// ================================================================
		{
			Name:        "Resource Exhaustion - Insufficient CPU/Memory",
			Description: "Pod cannot be scheduled because cluster has no nodes with sufficient resources",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)
				cluster.AddNode("node-2", true)

				pod := cluster.AddPod("ml-training-job", "data-science", "")
				pod.NodeName = "" // Not scheduled
				pod.Phase = "Pending"
				pod.Conditions["PodScheduled"] = MockCondition{
					Status: "False",
					Reason: "Unschedulable",
				}
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   3,
				PrimaryActors:   []string{"kube-scheduler"},
				RootInvariantID: "no_scheduling_failure",
				ShouldHaveChain: false,
			},
			Explanation: `
ROOT CAUSE: No nodes have sufficient CPU/memory to run the pod
REASON: Pod resource requests exceed available node capacity
IMPACT: Pod stuck in Pending state indefinitely
RESPONSIBILITY: kube-scheduler (cannot find suitable node)
FIX: Scale cluster (add nodes) OR reduce pod resource requests OR enable cluster autoscaler`,
		},

		// ================================================================
		// SCENARIO 3: CrashLoopBackOff - Application Error
		// ================================================================
		{
			Name:        "CrashLoopBackOff - Application Configuration Error",
			Description: "Container starts but immediately crashes due to missing environment variable",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("api-worker", "production", "node-1")
				pod.Phase = "Running"
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: "ContainersNotReady"}
				pod.ContainerStatus.Running = false
				pod.ContainerStatus.WaitingReason = "CrashLoopBackOff"
				pod.ContainerStatus.RestartCount = 5
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   2,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "no_crashloop",
			},
			Explanation: `
ROOT CAUSE: Application is crashing on startup
REASON: CrashLoopBackOff with 5 restarts indicates persistent application error
COMMON CAUSES: Missing environment variables, database unreachable, config file errors
RESPONSIBILITY: kubelet (reports crash) but this is APPLICATION code issue
FIX: Check application logs, verify environment variables, check dependencies (database, Redis, etc.)`,
		},

		// ================================================================
		// SCENARIO 4: Node Failure - Infrastructure Problem
		// ================================================================
		{
			Name:        "Node Failure - Network Partition",
			Description: "Node loses connection to control plane, affecting all pods on it",
			Setup: func(cluster *MockKubernetesCluster) {
				node := cluster.AddNode("worker-3", false)
				node.Conditions["Ready"] = "False"
				node.Conditions["NetworkUnavailable"] = "True"

				pod1 := cluster.AddPod("database-0", "production", "worker-3")
				pod1.Conditions["Ready"] = MockCondition{Status: "False", Reason: "NodeNotReady"}

				pod2 := cluster.AddPod("cache-redis", "production", "worker-3")
				pod2.Conditions["Ready"] = MockCondition{Status: "False", Reason: "NodeNotReady"}
			},
			Expected: ExpectedResults{
				ViolationCount:  4,
				CriticalCount:   3,
				PrimaryActors:   []string{"node-controller"},
				RootInvariantID: "node_ready",
			},
			Explanation: `
ROOT CAUSE: Node lost network connectivity
REASON: Node cannot communicate with Kubernetes control plane
IMPACT: All pods on this node become unavailable
RESPONSIBILITY: node-controller (detects failure) + infrastructure team
FIX: Check network connectivity, verify VPC/subnet configuration, check node health (SSH if possible)`,
		},

		// ================================================================
		// SCENARIO 5: Readiness Probe Failure - Application Not Ready
		// ================================================================
		{
			Name:        "Readiness Probe Failure - Database Connection Timeout",
			Description: "Application is running but failing readiness checks due to slow database",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("api-server", "production", "node-1")
				pod.Phase = "Running"
				pod.Conditions["Ready"] = MockCondition{
					Status: "False",
					Reason: "ContainersNotReady",
				}
				pod.ContainerStatus.Running = true // Container IS running
				pod.Labels["app"] = "api"

				cluster.AddService("api", "production", map[string]string{"app": "api"})
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   3,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "readiness_probe_success",
			},
			Explanation: `
ROOT CAUSE: Readiness probe is failing
REASON: Container is running but /health endpoint returns non-200 status
COMMON CAUSES: Database slow/unavailable, external API timeout, initialization not complete
IMPACT: Pod not added to Service endpoints → no traffic routed to pod
RESPONSIBILITY: kubelet (executes probe) but APPLICATION issue
FIX: Check application /health endpoint, verify database connectivity, check application logs`,
		},

		// ================================================================
		// SCENARIO 6: Complete Service Outage - Multi-Layer Failure
		// ================================================================
		{
			Name:        "Complete Service Outage - Deployment Rollout Gone Wrong",
			Description: "New deployment pushed bad image, all pods failing, service completely down",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)
				cluster.AddNode("node-2", true)

				// All 3 replicas have bad image
				for i := 1; i <= 3; i++ {
					pod := cluster.AddPod(fmt.Sprintf("frontend-%d", i), "production", "node-1")
					pod.Phase = "Pending"
					pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
					pod.ContainerStatus.Running = false
					pod.ContainerStatus.WaitingReason = "ErrImagePull"
					pod.Labels["app"] = "frontend"
				}

				svc := cluster.AddService("frontend", "production", map[string]string{"app": "frontend"})
				svc.Endpoints = 0
			},
			Expected: ExpectedResults{
				ViolationCount:  6,
				CriticalCount:   5,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "no_image_pull_error",
			},
			Explanation: `
ROOT CAUSE: Invalid container image deployed across all replicas
REASON: ErrImagePull indicates image name is malformed or registry unreachable
IMPACT: Complete service outage - ALL pods failing, NO endpoints available
BLAST RADIUS: All 3 replicas affected simultaneously
RESPONSIBILITY: kubelet (executor) + deployment pipeline (bad image reference)
FIX: ROLLBACK deployment immediately, fix image tag in deployment manifest`,
		},

		// ================================================================
		// SCENARIO 7: OOM Kill - Memory Leak
		// ================================================================
		{
			Name:        "OOM Killed - Memory Leak in Application",
			Description: "Container consuming excessive memory and being killed by kernel",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("analytics-processor", "data", "node-1")
				pod.Phase = "Running"
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
				pod.ContainerStatus.Running = false
				pod.ContainerStatus.WaitingReason = "OOMKilled"
				pod.ContainerStatus.RestartCount = 12
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   3,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "no_oom_killed",
			},
			Explanation: `
ROOT CAUSE: Container exceeded memory limit and was killed by Linux OOM killer
REASON: Application has memory leak or processing too much data
PATTERN: 12 restarts indicates repeated OOM kills
RESPONSIBILITY: kubelet (enforces limits) but APPLICATION issue (memory leak)
FIX: Increase memory limits (temporary), fix memory leak in code (permanent), add memory profiling`,
		},

		// ================================================================
		// SCENARIO 8: Node Resource Pressure - Cascading Issues
		// ================================================================
		{
			Name:        "Node Under Pressure - Disk Space Exhausted",
			Description: "Node running out of disk space, affecting pod scheduling and evictions",
			Setup: func(cluster *MockKubernetesCluster) {
				node := cluster.AddNode("worker-1", true)
				node.Conditions["DiskPressure"] = "True"

				pod1 := cluster.AddPod("logging-agent", "kube-system", "worker-1")
				pod1.Phase = "Running"
				pod1.Conditions["Ready"] = MockCondition{Status: "True", Reason: ""}

				// New pod cannot be scheduled due to disk pressure
				pod2 := cluster.AddPod("app-backend", "production", "worker-1")
				pod2.Phase = "Pending"
				pod2.Conditions["PodScheduled"] = MockCondition{Status: "False", Reason: ""}
			},
			Expected: ExpectedResults{
				ViolationCount: 1,
				CriticalCount:  0,
				PrimaryActors:  []string{"node-controller"},
			},
			Explanation: `
ROOT CAUSE: Node disk space is critically low
REASON: Logs, images, or ephemeral storage consuming too much disk
IMPACT: No new pods can be scheduled on this node, existing pods may be evicted
RESPONSIBILITY: node-controller (detects pressure) + infrastructure team
FIX: Clean up unused images (kubectl), rotate logs, increase disk size, add monitoring`,
		},

		// ================================================================
		// SCENARIO 9: Invalid Image Name - Typo in Deployment
		// ================================================================
		{
			Name:        "Invalid Image Name - Deployment Configuration Error",
			Description: "Container image name has typo or invalid format",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("web-server", "production", "node-1")
				pod.Phase = "Pending"
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
				pod.ContainerStatus.Running = false
				pod.ContainerStatus.WaitingReason = "InvalidImageName"
			},
			Expected: ExpectedResults{
				ViolationCount:  3,
				CriticalCount:   3,
				PrimaryActors:   []string{"kubelet"},
				RootInvariantID: "no_invalid_image",
			},
			Explanation: `
ROOT CAUSE: Container image name is malformed
REASON: Image name doesn't follow Docker naming convention
COMMON CAUSES: Typo in deployment YAML, missing registry prefix, invalid characters
RESPONSIBILITY: kubelet (validates image name) but CONFIGURATION error
FIX: Correct image name in deployment manifest, follow format: registry/repository:tag`,
		},

		// ================================================================
		// SCENARIO 10: Liveness Probe Failure - Deadlock in Application
		// ================================================================
		{
			Name:        "Liveness Probe Failure - Application Deadlock",
			Description: "Application enters deadlock state, liveness probe fails, container restarted",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("queue-worker", "jobs", "node-1")
				pod.Phase = "Running"
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
				pod.ContainerStatus.Running = false
				pod.ContainerStatus.WaitingReason = "LivenessProbeFailure"
				pod.ContainerStatus.RestartCount = 3
			},
			Expected: ExpectedResults{
				ViolationCount: 2,
				CriticalCount:  1,
				PrimaryActors:  []string{"kubelet"},
			},
			Explanation: `
ROOT CAUSE: Liveness probe is failing, causing container restarts
REASON: Application is unresponsive (deadlock, infinite loop, or hung process)
PATTERN: 3 restarts indicates Kubernetes is attempting recovery
RESPONSIBILITY: kubelet (executes probe and restarts) but APPLICATION issue
FIX: Debug application deadlock, add thread dumps, review liveness probe configuration`,
		},

		// ================================================================
		// SCENARIO 11: Healthy Production System
		// ================================================================
		{
			Name:        "Healthy Production System - Baseline",
			Description: "All components healthy, no issues detected",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)
				cluster.AddNode("node-2", true)

				for i := 1; i <= 5; i++ {
					pod := cluster.AddPod(fmt.Sprintf("api-%d", i), "production", "node-1")
					pod.Phase = "Running"
					pod.Conditions["Ready"] = MockCondition{Status: "True", Reason: ""}
					pod.ContainerStatus.Running = true
					pod.Labels["app"] = "api"
				}

				svc := cluster.AddService("api", "production", map[string]string{"app": "api"})
				svc.Endpoints = 5
			},
			Expected: ExpectedResults{
				ViolationCount: 0,
				CriticalCount:  0,
				PrimaryActors:  []string{},
			},
			Explanation: `
SYSTEM STATE: Healthy
ALL INVARIANTS: Satisfied
NODES: All ready
PODS: All running and ready
SERVICES: All have healthy endpoints
NO ACTION REQUIRED: System operating normally`,
		},

		// ================================================================
		// SCENARIO 12: PersistentVolumeClaim Not Bound
		// ================================================================
		{
			Name:        "Storage Issue - PVC Pending",
			Description: "Pod cannot start because PersistentVolumeClaim is not bound to a volume",
			Setup: func(cluster *MockKubernetesCluster) {
				cluster.AddNode("node-1", true)

				pod := cluster.AddPod("database-master", "production", "node-1")
				pod.Phase = "Pending"
				pod.Conditions["PodInitialized"] = MockCondition{
					Status: "False",
					Reason: "VolumeNotReady",
				}
				pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
			},
			Expected: ExpectedResults{
				ViolationCount: 2,
				CriticalCount:  2,
				PrimaryActors:  []string{"kubelet"},
			},
			Explanation: `
ROOT CAUSE: PersistentVolumeClaim cannot be bound to a PersistentVolume
REASON: No available PV matching the PVC requirements (size, access mode, storage class)
IMPACT: Pod stuck in Pending state, cannot mount required volumes
RESPONSIBILITY: pv-controller (binding) + storage team
FIX: Create matching PersistentVolume OR provision storage via StorageClass OR check PVC requirements`,
		},
	}
}

// ============================================================================
// TEST RUNNER
// ============================================================================

func RunTestScenarios(store state.StateStore, eng *engine.InvariantEngine) {
	scenarios := GetTestScenarios()

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("CAUSALITY ENGINE - TEST SUITE")
	fmt.Println(strings.Repeat("=", 80))

	passed := 0
	failed := 0

	for i, scenario := range scenarios {
		fmt.Printf("\n[TEST %d/%d] %s\n", i+1, len(scenarios), scenario.Name)
		fmt.Printf("Description: %s\n", scenario.Description)
		fmt.Println(strings.Repeat("-", 80))

		// Clear store (for memory store)
		if memStore, ok := store.(*state.MemoryStore); ok {
			*memStore = *state.NewMemoryStore()
		}

		// Setup cluster
		cluster := NewMockCluster()
		scenario.Setup(cluster)

		// Load events
		events := cluster.ToStateEvents()
		for _, event := range events {
			store.Record(event)
		}

		// Evaluate
		violations := eng.EvaluateAll()

		// Filter to only violated
		activeViolations := make([]*engine.ViolationResult, 0)
		criticalCount := 0
		for _, v := range violations {
			if v.Violated {
				activeViolations = append(activeViolations, v)
				if v.Severity == dsl.Critical {
					criticalCount++
				}
			}
		}

		// Verify expectations
		testPassed := true

		if len(activeViolations) != scenario.Expected.ViolationCount {
			fmt.Printf("❌ FAILED: Expected %d violations, got %d\n",
				scenario.Expected.ViolationCount, len(activeViolations))
			testPassed = false
		}

		if criticalCount != scenario.Expected.CriticalCount {
			fmt.Printf("❌ FAILED: Expected %d critical violations, got %d\n",
				scenario.Expected.CriticalCount, criticalCount)
			testPassed = false
		}

		// Check primary actors
		foundActors := make(map[string]bool)
		for _, v := range activeViolations {
			foundActors[v.ResponsibleActor] = true
		}

		for _, expectedActor := range scenario.Expected.PrimaryActors {
			if !foundActors[expectedActor] {
				fmt.Printf("❌ FAILED: Expected actor '%s' not found\n", expectedActor)
				testPassed = false
			}
		}

		if testPassed {
			fmt.Printf("✅ PASSED\n")
			passed++
		} else {
			failed++
		}

		// Show detailed output
		if len(activeViolations) > 0 {
			fmt.Println("\nDetected Violations:")
			for _, v := range activeViolations {
				fmt.Printf("  • %s [%s] - %s\n", v.InvariantID, v.Severity, v.ResponsibleActor)
			}

			// Show full explanation for first violation
			if len(activeViolations) > 0 {
				fmt.Println("\nFull Explanation (First Violation):")
				fmt.Println(formatting.FormatExplanation(activeViolations[0]))
			}
		} else {
			fmt.Println("✓ No violations detected (healthy state)")
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("TEST RESULTS: %d passed, %d failed out of %d total\n",
		passed, failed, len(scenarios))
	fmt.Println(strings.Repeat("=", 80) + "\n")
}

// ============================================================================
// INTEGRATION TEST WITH API
// ============================================================================

func RunAPIIntegrationTests(apiAddr string) {
	baseURL := "http://localhost" + apiAddr

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("API INTEGRATION TESTS")
	fmt.Println(strings.Repeat("=", 80) + "\n")

	tests := []struct {
		Name     string
		Method   string
		Endpoint string
		Body     string
		Check    func([]byte) bool
	}{
		{
			Name:     "Health Check",
			Method:   "GET",
			Endpoint: "/health",
			Check: func(body []byte) bool {
				return strings.Contains(string(body), "healthy") ||
					strings.Contains(string(body), "unhealthy")
			},
		},
		{
			Name:     "List Invariants",
			Method:   "GET",
			Endpoint: "/api/v1/invariants",
			Check: func(body []byte) bool {
				return strings.Contains(string(body), "pod_ready")
			},
		},
		{
			Name:     "Get Stats",
			Method:   "GET",
			Endpoint: "/api/v1/stats",
			Check: func(body []byte) bool {
				return strings.Contains(string(body), "total_invariants")
			},
		},
		{
			Name:     "Evaluate Invariants",
			Method:   "POST",
			Endpoint: "/api/v1/invariants/evaluate",
			Check: func(body []byte) bool {
				return strings.Contains(string(body), "violations")
			},
		},
	}

	passed := 0
	failed := 0

	for _, test := range tests {
		fmt.Printf("[API TEST] %s... ", test.Name)

		url := baseURL + test.Endpoint
		var resp *http.Response
		var err error

		if test.Method == "GET" {
			resp, err = http.Get(url)
		} else if test.Method == "POST" {
			resp, err = http.Post(url, "application/json", strings.NewReader(test.Body))
		}

		if err != nil {
			fmt.Printf("❌ FAILED (request error: %v)\n", err)
			failed++
			continue
		}

		body := make([]byte, 4096)
		n, _ := resp.Body.Read(body)
		body = body[:n]
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 && test.Check(body) {
			fmt.Printf("✅ PASSED\n")
			passed++
		} else {
			fmt.Printf("❌ FAILED (status: %d)\n", resp.StatusCode)
			failed++
		}
	}

	fmt.Printf("\nAPI Tests: %d passed, %d failed\n", passed, failed)
	fmt.Println(strings.Repeat("=", 80) + "\n")
}

// ============================================================================
// PERFORMANCE BENCHMARK
// ============================================================================

func RunPerformanceBenchmark(store state.StateStore, eng *engine.InvariantEngine) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("PERFORMANCE BENCHMARK")
	fmt.Println(strings.Repeat("=", 80) + "\n")

	fmt.Println("Generating test data...")

	// Generate test cluster
	cluster := NewMockCluster()

	// Add nodes
	for i := 0; i < 10; i++ {
		cluster.AddNode(fmt.Sprintf("node-%d", i), i%5 != 0)
	}

	// Add pods
	for i := 0; i < 1000; i++ {
		pod := cluster.AddPod(
			fmt.Sprintf("pod-%d", i),
			"default",
			fmt.Sprintf("node-%d", i%10),
		)
		if i%10 == 0 {
			pod.Phase = "Pending"
			pod.Conditions["Ready"] = MockCondition{Status: "False", Reason: ""}
		}
	}

	events := cluster.ToStateEvents()
	for _, event := range events {
		store.Record(event)
	}

	fmt.Println("✓ Generated 1000 pods and 10 nodes")

	// Benchmark evaluation
	fmt.Println("\nRunning evaluation benchmark...")
	start := time.Now()
	violations := eng.EvaluateAll()
	duration := time.Since(start)

	violatedCount := 0
	for _, v := range violations {
		if v.Violated {
			violatedCount++
		}
	}

	fmt.Printf("✓ Evaluated %d invariants across 1010 resources in %v\n",
		len(eng.GetInvariants()), duration)
	fmt.Printf("✓ Found %d violations\n", violatedCount)
	fmt.Printf("✓ Performance: %.2f evaluations/second\n",
		float64(len(eng.GetInvariants())*1010)/duration.Seconds())

	fmt.Println(strings.Repeat("=", 80) + "\n")

	// Display sample violations
	fmt.Println("=== SAMPLE VIOLATIONS ===")
	displayCount := 0
	for _, v := range violations {
		if v.Violated && displayCount < 3 {
			fmt.Println(formatting.FormatExplanation(v))

			jsonBytes, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println("\nJSON Output:")
			fmt.Println(string(jsonBytes))
			fmt.Println()

			displayCount++
		}
	}
}
