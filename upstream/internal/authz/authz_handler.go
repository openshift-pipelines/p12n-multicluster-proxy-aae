package authz

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// AuthzHandler handles authorization using SubjectAccessReview.
// It uses the service account's client for all API calls, avoiding
// per-request client construction.
type AuthzHandler struct {
	kubeClient kubernetes.Interface
}

// NewAuthzHandler creates a new AuthzHandler
func NewAuthzHandler(kubeClient kubernetes.Interface) *AuthzHandler {
	return &AuthzHandler{
		kubeClient: kubeClient,
	}
}

type resolvedIdentity struct {
	Username string
	UID      string
	Groups   []string
	Extra    map[string]authenticationv1.ExtraValue
}

// resolveToken validates a bearer token and returns the authenticated user
// identity via TokenReview, using the service account's client.
func (a *AuthzHandler) resolveToken(ctx context.Context, token string) (*resolvedIdentity, error) {
	tr := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: token,
		},
	}

	result, err := a.kubeClient.AuthenticationV1().TokenReviews().Create(ctx, tr, v1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("token review failed: %v", err)
	}

	if !result.Status.Authenticated {
		return nil, fmt.Errorf("token is not authenticated")
	}

	return &resolvedIdentity{
		Username: result.Status.User.Username,
		UID:      result.Status.User.UID,
		Groups:   result.Status.User.Groups,
		Extra:    result.Status.User.Extra,
	}, nil
}

// checkAccess performs a SubjectAccessReview for the given user against the
// specified resource attributes, using the service account's client.
func (a *AuthzHandler) checkAccess(ctx context.Context, id *resolvedIdentity, attrs *authorizationv1.ResourceAttributes) error {
	extra := make(map[string]authorizationv1.ExtraValue, len(id.Extra))
	for k, v := range id.Extra {
		extra[k] = authorizationv1.ExtraValue(v)
	}
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			ResourceAttributes: attrs,
			User:               id.Username,
			UID:                id.UID,
			Groups:             id.Groups,
			Extra:              extra,
		},
	}

	result, err := a.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("subject access review failed: %v", err)
	}

	if !result.Status.Allowed {
		return fmt.Errorf("access denied: %s", result.Status.Reason)
	}

	return nil
}

// CheckPipelineRunAccess checks if the caller can access a PipelineRun
func (a *AuthzHandler) CheckPipelineRunAccess(ctx context.Context, r *http.Request, namespace, pipelineRunName string) error {
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	id, err := a.resolveToken(ctx, callerToken)
	if err != nil {
		return err
	}

	if err := a.checkAccess(ctx, id, &authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "get",
		Group:     "tekton.dev",
		Version:   "v1",
		Resource:  "pipelineruns",
		Name:      pipelineRunName,
	}); err != nil {
		return err
	}

	klog.V(4).Infof("Access granted to PipelineRun %s/%s for user %s", namespace, pipelineRunName, id.Username)
	return nil
}

// CheckPodAccess checks if the caller can access a Pod
func (a *AuthzHandler) CheckPodAccess(ctx context.Context, r *http.Request, namespace, podName string) error {
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	id, err := a.resolveToken(ctx, callerToken)
	if err != nil {
		return err
	}

	if err := a.checkAccess(ctx, id, &authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "get",
		Group:     "",
		Version:   "v1",
		Resource:  "pods",
		Name:      podName,
	}); err != nil {
		return err
	}

	klog.V(4).Infof("Access granted to Pod %s/%s for user %s", namespace, podName, id.Username)
	return nil
}

// CheckPodLogsAccess checks if the caller can access pod logs
func (a *AuthzHandler) CheckPodLogsAccess(ctx context.Context, r *http.Request, namespace, podName string) error {
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	id, err := a.resolveToken(ctx, callerToken)
	if err != nil {
		return err
	}

	if err := a.checkAccess(ctx, id, &authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "get",
		Group:     "",
		Version:   "v1",
		Resource:  "pods/log",
		Name:      podName,
	}); err != nil {
		return err
	}

	klog.V(4).Infof("Access granted to Pod logs %s/%s for user %s", namespace, podName, id.Username)
	return nil
}

// CheckTaskRunListAccess checks if the caller can list TaskRuns in a namespace
func (a *AuthzHandler) CheckTaskRunListAccess(ctx context.Context, r *http.Request, namespace string) error {
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	id, err := a.resolveToken(ctx, callerToken)
	if err != nil {
		return err
	}

	if err := a.checkAccess(ctx, id, &authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "list",
		Group:     "tekton.dev",
		Version:   "v1",
		Resource:  "taskruns",
	}); err != nil {
		return err
	}

	klog.V(4).Infof("Access granted to list TaskRuns in %s for user %s", namespace, id.Username)
	return nil
}

// CheckPodListAccess checks if the caller can list Pods in a namespace
func (a *AuthzHandler) CheckPodListAccess(ctx context.Context, r *http.Request, namespace string) error {
	callerToken := a.extractBearerToken(r)
	if callerToken == "" {
		return fmt.Errorf("no authorization token provided")
	}

	id, err := a.resolveToken(ctx, callerToken)
	if err != nil {
		return err
	}

	if err := a.checkAccess(ctx, id, &authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      "list",
		Group:     "",
		Version:   "v1",
		Resource:  "pods",
	}); err != nil {
		return err
	}

	klog.V(4).Infof("Access granted to list Pods in %s for user %s", namespace, id.Username)
	return nil
}

// extractBearerToken extracts the bearer token from the Authorization header
func (a *AuthzHandler) extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}
