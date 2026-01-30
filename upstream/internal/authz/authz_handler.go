package authz

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// AuthzHandler handles authorization using SelfSubjectAccessReview
type AuthzHandler struct {
	kubeClient kubernetes.Interface
}

// NewAuthzHandler creates a new AuthzHandler
func NewAuthzHandler(kubeClient kubernetes.Interface) *AuthzHandler {
	return &AuthzHandler{
		kubeClient: kubeClient,
	}
}

// CheckPipelineRunAccess checks if the caller can access a PipelineRun
func (a *AuthzHandler) CheckPipelineRunAccess(ctx context.Context, r *http.Request, namespace, pipelineRunName string) error {
	// Extract caller's token from Authorization header
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	// Create a Kubernetes client with the caller's token
	callerConfig := &rest.Config{
		Host:        "https://kubernetes.default.svc", // Use in-cluster API server
		BearerToken: callerToken,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // For testing - should be configured properly in production
		},
	}

	callerClient, err := kubernetes.NewForConfig(callerConfig)
	if err != nil {
		return fmt.Errorf("failed to create caller client: %v", err)
	}

	// Create SelfSubjectAccessReview with caller's token
	ssar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "get",
				Group:     "tekton.dev",
				Version:   "v1",
				Resource:  "pipelineruns",
				Name:      pipelineRunName,
			},
		},
	}

	// Submit the review using caller's client
	result, err := callerClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create SelfSubjectAccessReview: %v", err)
	}

	if !result.Status.Allowed {
		return fmt.Errorf("access denied to PipelineRun %s/%s: %s", namespace, pipelineRunName, result.Status.Reason)
	}

	klog.V(4).Infof("Access granted to PipelineRun %s/%s for caller", namespace, pipelineRunName)
	return nil
}

// CheckPodAccess checks if the caller can access a Pod
func (a *AuthzHandler) CheckPodAccess(ctx context.Context, r *http.Request, namespace, podName string) error {
	// Extract caller's token from Authorization header
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	// Create a Kubernetes client with the caller's token
	callerConfig := &rest.Config{
		Host:        "https://kubernetes.default.svc", // Use in-cluster API server
		BearerToken: callerToken,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // For testing - should be configured properly in production
		},
	}

	callerClient, err := kubernetes.NewForConfig(callerConfig)
	if err != nil {
		return fmt.Errorf("failed to create caller client: %v", err)
	}

	// Create SelfSubjectAccessReview with caller's token
	ssar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "get",
				Group:     "",
				Version:   "v1",
				Resource:  "pods",
				Name:      podName,
			},
		},
	}

	// Submit the review using caller's client
	result, err := callerClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create SelfSubjectAccessReview: %v", err)
	}

	if !result.Status.Allowed {
		return fmt.Errorf("access denied to Pod %s/%s: %s", namespace, podName, result.Status.Reason)
	}

	klog.V(4).Infof("Access granted to Pod %s/%s for caller", namespace, podName)
	return nil
}

// CheckPodLogsAccess checks if the caller can access pod logs
func (a *AuthzHandler) CheckPodLogsAccess(ctx context.Context, r *http.Request, namespace, podName string) error {
	// Extract caller's token from Authorization header
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	// Create a Kubernetes client with the caller's token
	callerConfig := &rest.Config{
		Host:        "https://kubernetes.default.svc", // Use in-cluster API server
		BearerToken: callerToken,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // For testing - should be configured properly in production
		},
	}

	callerClient, err := kubernetes.NewForConfig(callerConfig)
	if err != nil {
		return fmt.Errorf("failed to create caller client: %v", err)
	}

	// Create SelfSubjectAccessReview with caller's token
	ssar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "get",
				Group:     "",
				Version:   "v1",
				Resource:  "pods/log",
				Name:      podName,
			},
		},
	}

	// Submit the review using caller's client
	result, err := callerClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create SelfSubjectAccessReview: %v", err)
	}

	if !result.Status.Allowed {
		return fmt.Errorf("access denied to Pod logs %s/%s: %s", namespace, podName, result.Status.Reason)
	}

	klog.V(4).Infof("Access granted to Pod logs %s/%s for caller", namespace, podName)
	return nil
}

// extractBearerToken extracts the bearer token from the Authorization header
func (a *AuthzHandler) extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Check if it's a bearer token
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}
