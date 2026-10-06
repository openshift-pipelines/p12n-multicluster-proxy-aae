package registry

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/config"
	tektonclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	kueuev1beta1 "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	kueueclient "sigs.k8s.io/kueue/client-go/clientset/versioned"
)

const (
	// MultiKueueClusterAnnotation is the annotation used to link secrets to MultiKueueCluster
	MultiKueueClusterAnnotation = "kueue.x-k8s.io/multikueue-cluster"
)

// WorkerConfigRegistry manages worker cluster configurations and cached clients
type WorkerConfigRegistry struct {
	kubeClient    kubernetes.Interface
	kueueClient   kueueclient.Interface
	config        *config.Config
	configs       map[string]*rest.Config
	kubeClients   map[string]kubernetes.Interface
	tektonClients map[string]tektonclient.Interface
	mu            sync.RWMutex
}

// NewWorkerConfigRegistry creates a new WorkerConfigRegistry
func NewWorkerConfigRegistry(kubeClient kubernetes.Interface, kueueClient kueueclient.Interface, config *config.Config) *WorkerConfigRegistry {
	registry := &WorkerConfigRegistry{
		kubeClient:    kubeClient,
		kueueClient:   kueueClient,
		config:        config,
		configs:       make(map[string]*rest.Config),
		kubeClients:   make(map[string]kubernetes.Interface),
		tektonClients: make(map[string]tektonclient.Interface),
	}

	// Start watching for MultiKueueCluster changes
	go registry.watchMultiKueueClusters()

	return registry
}

// GetConfig returns the kubeconfig for a worker cluster
func (r *WorkerConfigRegistry) GetConfig(clusterName string) (*rest.Config, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, exists := r.configs[clusterName]
	if !exists {
		return nil, fmt.Errorf("worker config not found for cluster: %s", clusterName)
	}
	return config, nil
}

// GetKubeClient returns a cached Kubernetes client for a worker cluster
func (r *WorkerConfigRegistry) GetKubeClient(clusterName string) (kubernetes.Interface, error) {
	r.mu.RLock()
	if client, ok := r.kubeClients[clusterName]; ok {
		r.mu.RUnlock()
		return client, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.kubeClients[clusterName]; ok {
		return client, nil
	}

	cfg, ok := r.configs[clusterName]
	if !ok {
		return nil, fmt.Errorf("worker config not found for cluster: %s", clusterName)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client for cluster %s: %v", clusterName, err)
	}

	r.kubeClients[clusterName] = client
	return client, nil
}

// GetTektonClient returns a cached Tekton client for a worker cluster
func (r *WorkerConfigRegistry) GetTektonClient(clusterName string) (tektonclient.Interface, error) {
	r.mu.RLock()
	if client, ok := r.tektonClients[clusterName]; ok {
		r.mu.RUnlock()
		return client, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.tektonClients[clusterName]; ok {
		return client, nil
	}

	cfg, ok := r.configs[clusterName]
	if !ok {
		return nil, fmt.Errorf("worker config not found for cluster: %s", clusterName)
	}

	client, err := tektonclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create tekton client for cluster %s: %v", clusterName, err)
	}

	r.tektonClients[clusterName] = client
	return client, nil
}

// LoadConfigs loads all worker configurations from MultiKueueCluster resources
func (r *WorkerConfigRegistry) LoadConfigs(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// List MultiKueueCluster resources
	clusters, err := r.kueueClient.KueueV1beta1().MultiKueueClusters().List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list MultiKueueClusters: %v", err)
	}

	// Clear cached clients since configs may have changed
	r.kubeClients = make(map[string]kubernetes.Interface)
	r.tektonClients = make(map[string]tektonclient.Interface)

	// Load each MultiKueueCluster
	for _, cluster := range clusters.Items {
		clusterName := cluster.Name

		// Get the secret name from the cluster spec
		if cluster.Spec.KubeConfig.LocationType != "Secret" {
			klog.Warningf("MultiKueueCluster %s has unsupported location type: %s", clusterName, cluster.Spec.KubeConfig.LocationType)
			continue
		}

		secretName := cluster.Spec.KubeConfig.Location
		if secretName == "" {
			klog.Warningf("MultiKueueCluster %s has empty secret location", clusterName)
			continue
		}

		// Get the secret
		secret, err := r.kubeClient.CoreV1().Secrets(r.config.WorkersSecretNamespace).Get(ctx, secretName, v1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get secret %s for cluster %s: %v", secretName, clusterName, err)
			continue
		}

		// Check if secret contains kubeconfig data
		kubeconfigData, exists := secret.Data["kubeconfig"]
		if !exists {
			klog.Warningf("Secret %s does not contain kubeconfig data", secretName)
			continue
		}

		// Parse kubeconfig using clientcmd
		cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
		if err != nil {
			klog.Errorf("Failed to parse kubeconfig for cluster %s: %v", clusterName, err)
			continue
		}

		cfg.QPS = r.config.ClientQPS
		cfg.Burst = r.config.ClientBurst

		r.configs[clusterName] = cfg
		klog.Infof("Loaded worker config for cluster: %s (secret: %s)", clusterName, secretName)
	}

	return nil
}

// watchMultiKueueClusters watches for changes to MultiKueueCluster resources
func (r *WorkerConfigRegistry) watchMultiKueueClusters() {
	ctx := context.Background()

	// Initial load
	if err := r.LoadConfigs(ctx); err != nil {
		klog.Errorf("Failed to load initial worker configs: %v", err)
	}

	// Watch for changes to MultiKueueCluster resources
	watcher, err := r.kueueClient.KueueV1beta1().MultiKueueClusters().Watch(ctx, v1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to watch MultiKueueClusters: %v", err)
		return
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		switch event.Type {
		case "ADDED", "MODIFIED":
			// Reload all configs when MultiKueueClusters change
			if err := r.LoadConfigs(ctx); err != nil {
				klog.Errorf("Failed to reload worker configs: %v", err)
			}
		case "DELETED":
			// Remove config and cached clients for deleted MultiKueueCluster
			if cluster, ok := event.Object.(*kueuev1beta1.MultiKueueCluster); ok {
				r.mu.Lock()
				delete(r.configs, cluster.Name)
				delete(r.kubeClients, cluster.Name)
				delete(r.tektonClients, cluster.Name)
				r.mu.Unlock()
				klog.Infof("Removed worker config for cluster: %s", cluster.Name)
			}
		}
	}
}

// ListClusters returns a list of available worker clusters
func (r *WorkerConfigRegistry) ListClusters() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clusters := make([]string, 0, len(r.configs))
	for clusterName := range r.configs {
		clusters = append(clusters, clusterName)
	}
	return clusters
}
