package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/config"
	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/resolver"
)

type mockAuthorizer struct {
	pipelineRunErr error
	podErr         error
	podLogsErr     error
	taskRunListErr error
	podListErr     error
}

func (m *mockAuthorizer) CheckPipelineRunAccess(_ context.Context, _ *http.Request, _, _ string) error {
	return m.pipelineRunErr
}

func (m *mockAuthorizer) CheckPodAccess(_ context.Context, _ *http.Request, _, _ string) error {
	return m.podErr
}

func (m *mockAuthorizer) CheckPodLogsAccess(_ context.Context, _ *http.Request, _, _ string) error {
	return m.podLogsErr
}

func (m *mockAuthorizer) CheckTaskRunListAccess(_ context.Context, _ *http.Request, _ string) error {
	return m.taskRunListErr
}

func (m *mockAuthorizer) CheckPodListAccess(_ context.Context, _ *http.Request, _ string) error {
	return m.podListErr
}

type mockResolver struct {
	cluster *resolver.WorkerCluster
	err     error
}

func (m *mockResolver) ResolveWorkerCluster(_ context.Context, _, _ string) (*resolver.WorkerCluster, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cluster, nil
}

type mockRegistry struct {
	cfg      *rest.Config
	clusters []string
	err      error
}

func (m *mockRegistry) GetConfig(_ string) (*rest.Config, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cfg, nil
}

func (m *mockRegistry) ListClusters() []string {
	return m.clusters
}

func fakeWorkerAPI(pods map[string]*corev1.Pod, logs map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle TaskRun listing: GET /apis/tekton.dev/v1/namespaces/{ns}/taskruns
		if strings.HasPrefix(r.URL.Path, "/apis/tekton.dev/v1/namespaces/") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"apiVersion":"tekton.dev/v1","kind":"TaskRunList","metadata":{},"items":[]}`)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/")
		parts := strings.Split(path, "/")

		// Pod listing: GET /api/v1/namespaces/{ns}/pods
		if len(parts) == 2 && parts[1] == "pods" {
			ns := parts[0]
			podList := &corev1.PodList{}
			for key, pod := range pods {
				if strings.HasPrefix(key, ns+"/") {
					podList.Items = append(podList.Items, *pod)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(podList)
			return
		}

		if len(parts) >= 3 && parts[1] == "pods" {
			key := parts[0] + "/" + parts[2]

			if len(parts) == 4 && parts[3] == "log" {
				if content, ok := logs[key]; ok {
					w.Header().Set("Content-Type", "text/plain")
					fmt.Fprint(w, content)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				return
			}

			if len(parts) == 3 {
				if pod, ok := pods[key]; ok {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(pod)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func newTestServer(workerURL string) *ProxyServer {
	return &ProxyServer{
		resolver: &mockResolver{
			cluster: &resolver.WorkerCluster{Name: "worker-1", State: "Admitted"},
		},
		workerRegistry: &mockRegistry{
			cfg:      &rest.Config{Host: workerURL},
			clusters: []string{"worker-1"},
		},
		authzHandler: &mockAuthorizer{},
		config:       &config.Config{DefaultLogTailLines: 100},
	}
}

func makePod(name, namespace string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestProxyServer_Health(t *testing.T) {
	server := &ProxyServer{config: &config.Config{}}

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.String() != "OK" {
		t.Errorf("got body %q, want %q", rr.Body.String(), "OK")
	}
}

func TestProxyServer_Ready_NoClusters(t *testing.T) {
	server := &ProxyServer{
		config:         &config.Config{},
		workerRegistry: &mockRegistry{clusters: []string{}},
	}

	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestProxyServer_Ready_WithClusters(t *testing.T) {
	server := &ProxyServer{
		config:         &config.Config{},
		workerRegistry: &mockRegistry{clusters: []string{"worker-1"}},
	}

	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestPodStatusOwnershipValidation(t *testing.T) {
	tests := []struct {
		name           string
		podLabels      map[string]string
		pipelineRun    string
		expectedStatus int
	}{
		{
			name:           "matching label allows access",
			podLabels:      map[string]string{"tekton.dev/pipelineRun": "my-pr"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-matching label denies access",
			podLabels:      map[string]string{"tekton.dev/pipelineRun": "other-pr"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "no labels denies access",
			podLabels:      nil,
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "unrelated labels only denies access",
			podLabels:      map[string]string{"app": "something"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pod := makePod("test-pod", "test-ns", tc.podLabels)
			worker := fakeWorkerAPI(map[string]*corev1.Pod{"test-ns/test-pod": pod}, nil)
			defer worker.Close()

			server := newTestServer(worker.URL)
			req := httptest.NewRequest("GET",
				"/api/v1/namespaces/test-ns/pods/test-pod/status?pipelineRun="+tc.pipelineRun, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			rr := httptest.NewRecorder()

			server.Handler().ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("got status %d, want %d; body: %s", rr.Code, tc.expectedStatus, rr.Body.String())
			}

			if tc.expectedStatus == http.StatusOK {
				var status corev1.PodStatus
				if err := json.NewDecoder(rr.Body).Decode(&status); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if status.Phase != corev1.PodRunning {
					t.Errorf("got phase %q, want %q", status.Phase, corev1.PodRunning)
				}
			}
		})
	}
}

func TestPodStatus_MissingPipelineRunParam(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	req := httptest.NewRequest("GET", "/api/v1/namespaces/test-ns/pods/test-pod/status", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestPodStatus_AuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{podErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pods/test-pod/status?pipelineRun=my-pr", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestLogsFetchOwnershipValidation(t *testing.T) {
	tests := []struct {
		name           string
		podLabels      map[string]string
		pipelineRun    string
		expectedStatus int
	}{
		{
			name:           "matching label allows access",
			podLabels:      map[string]string{"tekton.dev/pipelineRun": "my-pr"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-matching label denies access",
			podLabels:      map[string]string{"tekton.dev/pipelineRun": "other-pr"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "no labels denies access",
			podLabels:      nil,
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "unrelated labels only denies access",
			podLabels:      map[string]string{"app": "something"},
			pipelineRun:    "my-pr",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pod := makePod("test-pod", "test-ns", tc.podLabels)
			worker := fakeWorkerAPI(
				map[string]*corev1.Pod{"test-ns/test-pod": pod},
				map[string]string{"test-ns/test-pod": "log line 1\nlog line 2\n"},
			)
			defer worker.Close()

			server := newTestServer(worker.URL)
			req := httptest.NewRequest("GET",
				"/api/v1/namespaces/test-ns/logs?pod=test-pod&container=step-main&pipelineRun="+tc.pipelineRun, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			rr := httptest.NewRecorder()

			server.Handler().ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("got status %d, want %d; body: %s", rr.Code, tc.expectedStatus, rr.Body.String())
			}

			if tc.expectedStatus == http.StatusOK {
				if !strings.Contains(rr.Body.String(), "log line 1") {
					t.Errorf("expected log content in response, got: %s", rr.Body.String())
				}
			}
		})
	}
}

func TestLogsRequest_MissingPodParam(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/logs?pipelineRun=my-pr", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestLogsRequest_MissingPipelineRunParam(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/logs?pod=test-pod&container=step-main", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestLogsRequest_AuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{podLogsErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/logs?pod=test-pod&container=step-main&pipelineRun=my-pr", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestPodStatus_WorkerClusterNotAdmitted(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.resolver = &mockResolver{
		cluster: &resolver.WorkerCluster{Name: "worker-1", State: "Dispatching"},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pods/test-pod/status?pipelineRun=my-pr", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestNamespaceRequest_InvalidPath(t *testing.T) {
	server := &ProxyServer{config: &config.Config{}, authzHandler: &mockAuthorizer{}}

	req := httptest.NewRequest("GET", "/api/v1/namespaces/test-ns", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestNamespaceRequest_UnknownResource(t *testing.T) {
	server := &ProxyServer{config: &config.Config{}, authzHandler: &mockAuthorizer{}}

	req := httptest.NewRequest("GET", "/api/v1/namespaces/test-ns/unknown", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestPipelineRunRequest_AuthDenied(t *testing.T) {
	server := &ProxyServer{
		config:       &config.Config{},
		authzHandler: &mockAuthorizer{pipelineRunErr: fmt.Errorf("access denied")},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/resolve", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestPipelineRunRequest_UnknownSubResource(t *testing.T) {
	server := &ProxyServer{
		config:       &config.Config{},
		authzHandler: &mockAuthorizer{},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/unknown", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestPodRequest_UnknownSubResource(t *testing.T) {
	server := &ProxyServer{
		config:       &config.Config{},
		authzHandler: &mockAuthorizer{},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pods/my-pod/unknown", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestResolve_Admitted(t *testing.T) {
	server := &ProxyServer{
		resolver: &mockResolver{
			cluster: &resolver.WorkerCluster{Name: "worker-1", State: "Admitted", WorkloadName: "wl-1"},
		},
		authzHandler: &mockAuthorizer{},
		config:       &config.Config{},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/resolve", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	if got := rr.Header().Get("X-Worker-Cluster"); got != "worker-1" {
		t.Errorf("got X-Worker-Cluster %q, want %q", got, "worker-1")
	}

	var wc resolver.WorkerCluster
	if err := json.NewDecoder(rr.Body).Decode(&wc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if wc.State != "Admitted" {
		t.Errorf("got state %q, want %q", wc.State, "Admitted")
	}
	if wc.Name != "worker-1" {
		t.Errorf("got name %q, want %q", wc.Name, "worker-1")
	}
}

func TestResolve_Dispatching(t *testing.T) {
	server := &ProxyServer{
		resolver: &mockResolver{
			cluster: &resolver.WorkerCluster{State: "Dispatching", WorkloadName: "wl-1"},
		},
		authzHandler: &mockAuthorizer{},
		config:       &config.Config{},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/resolve", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusConflict)
	}

	var wc resolver.WorkerCluster
	if err := json.NewDecoder(rr.Body).Decode(&wc); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if wc.State != "Dispatching" {
		t.Errorf("got state %q, want %q", wc.State, "Dispatching")
	}
}

func TestResolve_Error(t *testing.T) {
	server := &ProxyServer{
		resolver: &mockResolver{
			err: fmt.Errorf("workload not found"),
		},
		authzHandler: &mockAuthorizer{},
		config:       &config.Config{},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/resolve", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestPipelineRunPods_AuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{podListErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/pods", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestTaskRuns_Success(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/taskruns", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	if got := rr.Header().Get("X-Worker-Cluster"); got != "worker-1" {
		t.Errorf("got X-Worker-Cluster %q, want %q", got, "worker-1")
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got Content-Type %q, want %q", ct, "application/json")
	}
}

func TestTaskRuns_AuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{taskRunListErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/taskruns", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestGetWorkerConfig_ResolverError(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.resolver = &mockResolver{err: fmt.Errorf("resolve failed")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/pods", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestGetWorkerConfig_RegistryError(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.workerRegistry = &mockRegistry{
		err:      fmt.Errorf("config not found"),
		clusters: []string{"worker-1"},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pipelineruns/my-pr/pods", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusFailedDependency {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusFailedDependency)
	}
}

func TestLogs_WorkerClusterNotAdmitted(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.resolver = &mockResolver{
		cluster: &resolver.WorkerCluster{Name: "worker-1", State: "Dispatching"},
	}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/logs?pod=test-pod&container=step-main&pipelineRun=my-pr", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestLogs_PipelineRunAuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{pipelineRunErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/logs?pod=test-pod&container=step-main&pipelineRun=my-pr", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestPodStatus_PipelineRunAuthDenied(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.authzHandler = &mockAuthorizer{pipelineRunErr: fmt.Errorf("access denied")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pods/test-pod/status?pipelineRun=my-pr", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestPodStatus_ResolverError(t *testing.T) {
	worker := fakeWorkerAPI(nil, nil)
	defer worker.Close()

	server := newTestServer(worker.URL)
	server.resolver = &mockResolver{err: fmt.Errorf("resolve failed")}

	req := httptest.NewRequest("GET",
		"/api/v1/namespaces/test-ns/pods/test-pod/status?pipelineRun=my-pr", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusNotFound)
	}
}
