package resolver

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/config"
	kueuev1beta1 "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	kueueclient "sigs.k8s.io/kueue/client-go/clientset/versioned"
)

const (
	// PipelineRunAnnotation is the annotation used to link Workloads to PipelineRuns
	PipelineRunAnnotation = "proxy.tekton.dev/pipelineRun"
)

// WorkloadResolver resolves worker clusters from Kueue Workload status
type WorkloadResolver struct {
	kueueClient kueueclient.Interface
	kubeClient  kubernetes.Interface
	config      *config.Config
}

// WorkerCluster represents a resolved worker cluster
type WorkerCluster struct {
	Name              string   `json:"name,omitempty"`
	State             string   `json:"state"`
	NominatedClusters []string `json:"nominatedClusters,omitempty"`
	WorkloadName      string   `json:"workloadName,omitempty"`
}

// NewWorkloadResolver creates a new WorkloadResolver
func NewWorkloadResolver(kubeClient kubernetes.Interface, kueueClient kueueclient.Interface, config *config.Config) *WorkloadResolver {
	return &WorkloadResolver{
		kueueClient: kueueClient,
		kubeClient:  kubeClient,
		config:      config,
	}
}

// ResolveWorkerCluster resolves the worker cluster for a given PipelineRun
func (r *WorkloadResolver) ResolveWorkerCluster(ctx context.Context, namespace, pipelineRunName string) (*WorkerCluster, error) {
	// Find Workload by annotation
	workload, err := r.findWorkloadByPipelineRun(ctx, namespace, pipelineRunName)
	if err != nil {
		return nil, fmt.Errorf("failed to find workload for PipelineRun %s/%s: %v", namespace, pipelineRunName, err)
	}

	if workload == nil {
		return nil, fmt.Errorf("no workload found for PipelineRun %s/%s", namespace, pipelineRunName)
	}

	// Check if workload is admitted
	if workload.Status.Admission != nil {
		clusterName := ""
		if workload.Status.ClusterName != nil {
			clusterName = *workload.Status.ClusterName
		}
		return &WorkerCluster{
			Name:         clusterName,
			State:        "Admitted",
			WorkloadName: workload.Name,
		}, nil
	}

	// Workload is still pending
	return &WorkerCluster{
		State:        "Dispatching",
		WorkloadName: workload.Name,
	}, nil
}

// findWorkloadByPipelineRun finds a Workload linked to a PipelineRun via owner reference
func (r *WorkloadResolver) findWorkloadByPipelineRun(ctx context.Context, namespace, pipelineRunName string) (*kueuev1beta1.Workload, error) {
	// List all Workloads in the namespace
	workloads, err := r.kueueClient.KueueV1beta1().Workloads(namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list workloads: %v", err)
	}

	// Find workload with matching owner reference
	for _, workload := range workloads.Items {
		for _, ownerRef := range workload.OwnerReferences {
			if ownerRef.Kind == "PipelineRun" && ownerRef.Name == pipelineRunName {
				return &workload, nil
			}
		}
	}

	return nil, nil
}

// GetWorkloadStatus returns the current status of a workload
func (r *WorkloadResolver) GetWorkloadStatus(ctx context.Context, namespace, workloadName string) (*kueuev1beta1.Workload, error) {
	return r.kueueClient.KueueV1beta1().Workloads(namespace).Get(ctx, workloadName, v1.GetOptions{})
}
