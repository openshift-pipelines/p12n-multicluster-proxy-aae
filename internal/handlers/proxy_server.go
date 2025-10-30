package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/gorilla/websocket"
	"github.com/khrm/proxy-aae/internal/authz"
	"github.com/khrm/proxy-aae/internal/config"
	"github.com/khrm/proxy-aae/internal/registry"
	"github.com/khrm/proxy-aae/internal/resolver"
	tektonclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
)

// ProxyServer handles HTTP requests and proxies them to worker clusters
type ProxyServer struct {
	resolver       *resolver.WorkloadResolver
	workerRegistry *registry.WorkerConfigRegistry
	authzHandler   *authz.AuthzHandler
	config         *config.Config
}

// NewProxyServer creates a new ProxyServer
func NewProxyServer(
	resolver *resolver.WorkloadResolver,
	workerRegistry *registry.WorkerConfigRegistry,
	authzHandler *authz.AuthzHandler,
	config *config.Config,
) *ProxyServer {
	return &ProxyServer{
		resolver:       resolver,
		workerRegistry: workerRegistry,
		authzHandler:   authzHandler,
		config:         config,
	}
}

// Handler returns the HTTP handler for the proxy server
func (p *ProxyServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/v1/namespaces/", p.handleNamespaceRequest)
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/ready", p.handleReady)

	return mux
}

// handleNamespaceRequest handles requests to /api/v1/namespaces/{namespace}/...
func (p *ProxyServer) handleNamespaceRequest(w http.ResponseWriter, r *http.Request) {
	// Parse URL path: /api/v1/namespaces/{namespace}/...
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/"), "/")
	if len(pathParts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	namespace := pathParts[0]
	resourcePath := strings.Join(pathParts[1:], "/")

	// Route based on resource path
	switch {
	case strings.HasPrefix(resourcePath, "pipelineruns/"):
		p.handlePipelineRunRequest(w, r, namespace, resourcePath)
	case strings.HasPrefix(resourcePath, "pods/"):
		p.handlePodRequest(w, r, namespace, resourcePath)
	case strings.HasPrefix(resourcePath, "logs"):
		p.handleLogsRequest(w, r, namespace, resourcePath)
	default:
		http.Error(w, "Unknown resource", http.StatusNotFound)
	}
}

// handlePipelineRunRequest handles PipelineRun-related requests
func (p *ProxyServer) handlePipelineRunRequest(w http.ResponseWriter, r *http.Request, namespace, resourcePath string) {
	// Parse PipelineRun name from path
	pathParts := strings.Split(resourcePath, "/")
	if len(pathParts) < 2 {
		http.Error(w, "Invalid PipelineRun path", http.StatusBadRequest)
		return
	}

	pipelineRunName := pathParts[1]
	subPath := strings.Join(pathParts[2:], "/")

	// Check authorization
	if err := p.authzHandler.CheckPipelineRunAccess(r.Context(), r, namespace, pipelineRunName); err != nil {
		http.Error(w, fmt.Sprintf("Access denied: %v", err), http.StatusForbidden)
		return
	}

	// Route to specific handler
	switch subPath {
	case "resolve":
		p.handleResolve(w, r, namespace, pipelineRunName)
	case "taskruns":
		p.handleTaskRuns(w, r, namespace, pipelineRunName)
	case "pods":
		p.handlePipelineRunPods(w, r, namespace, pipelineRunName)
	default:
		http.Error(w, "Unknown PipelineRun sub-resource", http.StatusNotFound)
	}
}

// handleResolve handles /resolve endpoint
func (p *ProxyServer) handleResolve(w http.ResponseWriter, r *http.Request, namespace, pipelineRunName string) {
	// Resolve worker cluster
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s/%s: %v", namespace, pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusNotFound)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	if workerCluster.Name != "" {
		w.Header().Set("X-Worker-Cluster", workerCluster.Name)
	}

	// Return appropriate status code
	if workerCluster.State == "Dispatching" {
		w.WriteHeader(http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// Write response
	if err := json.NewEncoder(w).Encode(workerCluster); err != nil {
		klog.Errorf("Failed to encode response: %v", err)
	}
}

// handleTaskRuns handles /taskruns endpoint
func (p *ProxyServer) handleTaskRuns(w http.ResponseWriter, r *http.Request, namespace, pipelineRunName string) {
	// Resolve worker cluster
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s/%s: %v", namespace, pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusNotFound)
		return
	}

	if workerCluster.State != "Admitted" {
		http.Error(w, "PipelineRun not admitted to worker cluster", http.StatusConflict)
		return
	}

	// Get worker config
	workerConfig, err := p.workerRegistry.GetConfig(workerCluster.Name)
	if err != nil {
		klog.Errorf("Failed to get worker config for cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Worker config not found: %v", err), http.StatusFailedDependency)
		return
	}

	// Create Tekton client for worker cluster
	tektonClient := tektonclient.NewForConfigOrDie(workerConfig)

	// List TaskRuns with label selector
	labelSelector := fmt.Sprintf("tekton.dev/pipelineRun=%s", pipelineRunName)
	taskRuns, err := tektonClient.TektonV1().TaskRuns(namespace).List(r.Context(), v1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		klog.Errorf("Failed to list TaskRuns from worker cluster: %v", err)
		http.Error(w, fmt.Sprintf("Failed to list TaskRuns: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Worker-Cluster", workerCluster.Name)

	// Write response
	if err := json.NewEncoder(w).Encode(taskRuns); err != nil {
		klog.Errorf("Failed to encode TaskRuns response: %v", err)
	}
}

// handlePipelineRunPods handles /pods endpoint for PipelineRun
func (p *ProxyServer) handlePipelineRunPods(w http.ResponseWriter, r *http.Request, namespace, pipelineRunName string) {
	// Resolve worker cluster
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s/%s: %v", namespace, pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusNotFound)
		return
	}

	if workerCluster.State != "Admitted" {
		http.Error(w, "PipelineRun not admitted to worker cluster", http.StatusConflict)
		return
	}

	// Get worker config
	workerConfig, err := p.workerRegistry.GetConfig(workerCluster.Name)
	if err != nil {
		klog.Errorf("Failed to get worker config for cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Worker config not found: %v", err), http.StatusFailedDependency)
		return
	}

	// Create Kubernetes client for worker cluster
	kubeClient := kubernetes.NewForConfigOrDie(workerConfig)

	// List Pods with label selector
	labelSelector := fmt.Sprintf("tekton.dev/pipelineRun=%s", pipelineRunName)
	pods, err := kubeClient.CoreV1().Pods(namespace).List(r.Context(), v1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		klog.Errorf("Failed to list Pods from worker cluster: %v", err)
		http.Error(w, fmt.Sprintf("Failed to list Pods: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Worker-Cluster", workerCluster.Name)

	// Write response
	if err := json.NewEncoder(w).Encode(pods); err != nil {
		klog.Errorf("Failed to encode Pods response: %v", err)
	}
}

// handlePodRequest handles Pod-related requests
func (p *ProxyServer) handlePodRequest(w http.ResponseWriter, r *http.Request, namespace, resourcePath string) {
	// Parse Pod name from path
	pathParts := strings.Split(resourcePath, "/")
	if len(pathParts) < 2 {
		http.Error(w, "Invalid Pod path", http.StatusBadRequest)
		return
	}

	podName := pathParts[1]
	subPath := strings.Join(pathParts[2:], "/")

	// Check authorization
	if err := p.authzHandler.CheckPodAccess(r.Context(), r, namespace, podName); err != nil {
		http.Error(w, fmt.Sprintf("Access denied: %v", err), http.StatusForbidden)
		return
	}

	// Route to specific handler
	switch subPath {
	case "status":
		p.handlePodStatus(w, r, namespace, podName)
	default:
		http.Error(w, "Unknown Pod sub-resource", http.StatusNotFound)
	}
}

// handlePodStatus handles /pods/{pod}/status endpoint
func (p *ProxyServer) handlePodStatus(w http.ResponseWriter, r *http.Request, namespace, podName string) {
	// Extract PipelineRun name from query parameter
	pipelineRunName := r.URL.Query().Get("pipelineRun")
	if pipelineRunName == "" {
		http.Error(w, "PipelineRun name must be provided as query parameter 'pipelineRun'", http.StatusBadRequest)
		return
	}

	// Resolve worker cluster using PipelineRun
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s: %v", pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusInternalServerError)
		return
	}

	if workerCluster.State != "Admitted" {
		http.Error(w, "PipelineRun not admitted to worker cluster", http.StatusConflict)
		return
	}

	// Get worker config
	workerConfig, err := p.workerRegistry.GetConfig(workerCluster.Name)
	if err != nil {
		klog.Errorf("Failed to get worker config for cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Worker config not found: %v", err), http.StatusFailedDependency)
		return
	}

	// Create Kubernetes client for the worker cluster
	kubeClient := kubernetes.NewForConfigOrDie(workerConfig)

	// Get pod status from worker cluster
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(r.Context(), podName, v1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get pod %s from worker cluster %s: %v", podName, workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Failed to get pod: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Worker-Cluster", workerCluster.Name)

	// Write response
	if err := json.NewEncoder(w).Encode(pod); err != nil {
		klog.Errorf("Failed to encode pod response: %v", err)
	}
}

// handleLogsRequest handles logs-related requests
func (p *ProxyServer) handleLogsRequest(w http.ResponseWriter, r *http.Request, namespace, resourcePath string) {
	// Parse query parameters
	podName := r.URL.Query().Get("pod")
	containerName := r.URL.Query().Get("container")

	if podName == "" {
		http.Error(w, "Pod name is required", http.StatusBadRequest)
		return
	}

	// Check authorization
	if err := p.authzHandler.CheckPodLogsAccess(r.Context(), r, namespace, podName); err != nil {
		http.Error(w, fmt.Sprintf("Access denied: %v", err), http.StatusForbidden)
		return
	}

	// Route to specific handler
	if strings.HasSuffix(resourcePath, "/stream") {
		p.handleLogsStream(w, r, namespace, podName, containerName)
	} else {
		p.handleLogsFetch(w, r, namespace, podName, containerName)
	}
}

// handleLogsFetch handles HTTP logs fetching
func (p *ProxyServer) handleLogsFetch(w http.ResponseWriter, r *http.Request, namespace, podName, containerName string) {
	// Parse query parameters
	sinceSeconds := int64(0)
	if sinceStr := r.URL.Query().Get("sinceSeconds"); sinceStr != "" {
		if since, err := strconv.ParseInt(sinceStr, 10, 64); err == nil {
			sinceSeconds = since
		}
	}

	tailLines := int64(p.config.DefaultLogTailLines)
	if tailStr := r.URL.Query().Get("tailLines"); tailStr != "" {
		if tail, err := strconv.ParseInt(tailStr, 10, 64); err == nil {
			tailLines = tail
		}
	}

	// Extract PipelineRun name from the route parameter
	// The logs endpoint should be called as: /api/v1/namespaces/{ns}/pipelineruns/{name}/logs?pod={podName}&container={container}
	// But for now, we'll extract it from the URL path
	// TODO: Update the route to include PipelineRun name as a path parameter
	pipelineRunName := r.URL.Query().Get("pipelineRun")
	if pipelineRunName == "" {
		http.Error(w, "PipelineRun name must be provided as query parameter 'pipelineRun'", http.StatusBadRequest)
		return
	}

	// Resolve worker cluster using PipelineRun
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s: %v", pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusInternalServerError)
		return
	}

	if workerCluster.State != "Admitted" {
		http.Error(w, "PipelineRun not admitted to worker cluster", http.StatusConflict)
		return
	}

	// Get worker config
	workerConfig, err := p.workerRegistry.GetConfig(workerCluster.Name)
	if err != nil {
		klog.Errorf("Failed to get worker config for cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Worker config not found: %v", err), http.StatusFailedDependency)
		return
	}

	// Create Kubernetes client for the worker cluster
	kubeClient := kubernetes.NewForConfigOrDie(workerConfig)

	// Set up log options
	logOptions := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}
	if sinceSeconds > 0 {
		sinceTime := v1.NewTime(v1.Now().Add(-time.Duration(sinceSeconds) * time.Second))
		logOptions.SinceTime = &sinceTime
	}

	// Get logs from the worker cluster
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(r.Context())
	if err != nil {
		klog.Errorf("Failed to get logs from worker cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Failed to get logs: %v", err), http.StatusInternalServerError)
		return
	}
	defer logs.Close()

	// Set response headers
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Worker-Cluster", workerCluster.Name)
	w.WriteHeader(http.StatusOK)

	// Stream logs to client
	_, err = io.Copy(w, logs)
	if err != nil {
		klog.Errorf("Failed to stream logs: %v", err)
	}
}

// handleLogsStream handles WebSocket logs streaming
func (p *ProxyServer) handleLogsStream(w http.ResponseWriter, r *http.Request, namespace, podName, containerName string) {
	// Extract PipelineRun name from query parameter
	pipelineRunName := r.URL.Query().Get("pipelineRun")
	if pipelineRunName == "" {
		http.Error(w, "PipelineRun name must be provided as query parameter 'pipelineRun'", http.StatusBadRequest)
		return
	}

	// Resolve worker cluster using PipelineRun
	workerCluster, err := p.resolver.ResolveWorkerCluster(r.Context(), namespace, pipelineRunName)
	if err != nil {
		klog.Errorf("Failed to resolve worker cluster for PipelineRun %s: %v", pipelineRunName, err)
		http.Error(w, fmt.Sprintf("Failed to resolve worker cluster: %v", err), http.StatusInternalServerError)
		return
	}

	if workerCluster.State != "Admitted" {
		http.Error(w, "PipelineRun not admitted to worker cluster", http.StatusConflict)
		return
	}

	// Get worker config
	workerConfig, err := p.workerRegistry.GetConfig(workerCluster.Name)
	if err != nil {
		klog.Errorf("Failed to get worker config for cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Worker config not found: %v", err), http.StatusFailedDependency)
		return
	}

	// Create Kubernetes client for the worker cluster
	kubeClient := kubernetes.NewForConfigOrDie(workerConfig)

	// Set up log options for streaming
	logOptions := &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true, // Enable streaming
	}

	// Get logs stream from the worker cluster
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(r.Context())
	if err != nil {
		klog.Errorf("Failed to get logs stream from worker cluster %s: %v", workerCluster.Name, err)
		http.Error(w, fmt.Sprintf("Failed to get logs stream: %v", err), http.StatusInternalServerError)
		return
	}
	defer logs.Close()

	// Upgrade to WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to upgrade to WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// Set WebSocket headers
	conn.SetWriteDeadline(time.Now().Add(24 * time.Hour)) // 24 hour timeout

	// Stream logs to WebSocket client
	buffer := make([]byte, 1024)
	for {
		n, err := logs.Read(buffer)
		if err != nil {
			if err != io.EOF {
				klog.Errorf("Error reading logs stream: %v", err)
			}
			break
		}

		// Send log data to WebSocket client
		if err := conn.WriteMessage(websocket.TextMessage, buffer[:n]); err != nil {
			klog.Errorf("Error writing to WebSocket: %v", err)
			break
		}
	}
}

// handleHealth handles health check endpoint
func (p *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleReady handles readiness check endpoint
func (p *ProxyServer) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check if worker registry is ready
	clusters := p.workerRegistry.ListClusters()
	if len(clusters) == 0 {
		http.Error(w, "No worker clusters configured", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}
