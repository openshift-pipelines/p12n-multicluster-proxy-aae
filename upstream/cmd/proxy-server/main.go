package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	kueueclient "sigs.k8s.io/kueue/client-go/clientset/versioned"

	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/authz"
	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/config"
	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/handlers"
	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/registry"
	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/resolver"
)

func main() {
	// Parse command line flags
	var (
		port                = flag.String("port", "8080", "Port to listen on")
		workersSecretNS     = flag.String("workers-secret-namespace", "kueue-system", "Namespace for worker kubeconfig secrets")
		requestTimeout      = flag.Duration("request-timeout", 30*time.Second, "Timeout for worker cluster requests")
		defaultLogTailLines = flag.Int("default-log-tail-lines", 100, "Default number of log lines to tail")
		kubeconfig          = flag.String("kubeconfig", "", "Path to kubeconfig file")
	)
	flag.Parse()

	// Initialize klog
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Set("v", "2")

	// Load Kubernetes configuration
	cfg, err := loadKubeConfig(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to load kubeconfig: %v", err)
	}

	// Create Kubernetes clients
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// Create Kueue client
	kueueClient, err := kueueclient.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create kueue client: %v", err)
	}

	// Create configuration
	appConfig := &config.Config{
		WorkersSecretNamespace: *workersSecretNS,
		RequestTimeout:         *requestTimeout,
		DefaultLogTailLines:    *defaultLogTailLines,
	}

	// Initialize components
	workloadResolver := resolver.NewWorkloadResolver(kubeClient, kueueClient, appConfig)
	workerRegistry := registry.NewWorkerConfigRegistry(kubeClient, kueueClient, appConfig)
	authzHandler := authz.NewAuthzHandler(kubeClient)

	// Create proxy server
	proxyServer := handlers.NewProxyServer(workloadResolver, workerRegistry, authzHandler, appConfig)

	// Start HTTP server
	server := &http.Server{
		Addr:         ":" + *port,
		Handler:      proxyServer.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	klog.Infof("Starting proxy server on port %s", *port)
	klog.Infof("Workers secret namespace: %s", *workersSecretNS)
	klog.Infof("Request timeout: %v", *requestTimeout)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func loadKubeConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	// Fall back to default kubeconfig
	return clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
}
