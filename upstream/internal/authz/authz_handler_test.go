package authz

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestCheckPipelineRunAccessUsesAuthenticatedIdentity(t *testing.T) {
	tokenReviewed := false
	accessReviewed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/authentication.k8s.io/v1/tokenreviews":
			var review authenticationv1.TokenReview
			if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
				t.Fatal(err)
			}
			if review.Spec.Token != "caller-token" {
				t.Errorf("TokenReview token = %q, want caller-token", review.Spec.Token)
			}
			tokenReviewed = true
			writeJSON(t, w, authenticationv1.TokenReview{
				TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenReview"},
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: true,
					User: authenticationv1.UserInfo{
						Username: "alice",
						UID:      "alice-uid",
						Groups:   []string{"developers"},
						Extra: map[string]authenticationv1.ExtraValue{
							"scopes": {"builds:read"},
						},
					},
				},
			})
		case "/apis/authorization.k8s.io/v1/subjectaccessreviews":
			var review authorizationv1.SubjectAccessReview
			if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
				t.Fatal(err)
			}
			if review.Spec.User != "alice" || review.Spec.UID != "alice-uid" {
				t.Errorf("SubjectAccessReview identity = %q/%q, want alice/alice-uid", review.Spec.User, review.Spec.UID)
			}
			if len(review.Spec.Groups) != 1 || review.Spec.Groups[0] != "developers" {
				t.Errorf("SubjectAccessReview groups = %v, want [developers]", review.Spec.Groups)
			}
			scopes := review.Spec.Extra["scopes"]
			if len(scopes) != 1 || scopes[0] != "builds:read" {
				t.Errorf("SubjectAccessReview scopes = %v, want [builds:read]", scopes)
			}
			attrs := review.Spec.ResourceAttributes
			if attrs == nil || attrs.Namespace != "builds" || attrs.Resource != "pipelineruns" || attrs.Name != "run-1" || attrs.Verb != "get" {
				t.Errorf("SubjectAccessReview attributes = %#v", attrs)
			}
			accessReviewed = true
			writeJSON(t, w, authorizationv1.SubjectAccessReview{
				TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SubjectAccessReview"},
				Status:   authorizationv1.SubjectAccessReviewStatus{Allowed: true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newKubeClient(t, server.URL, certificatePEM(server.Certificate()))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	if err := NewAuthzHandler(client).CheckPipelineRunAccess(req.Context(), req, "builds", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !tokenReviewed || !accessReviewed {
		t.Fatalf("TokenReview performed = %t, SubjectAccessReview performed = %t", tokenReviewed, accessReviewed)
	}
}

func TestCheckPipelineRunAccessRejectsUntrustedTLS(t *testing.T) {
	called := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	client := newKubeClient(t, server.URL, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	if err := NewAuthzHandler(client).CheckPipelineRunAccess(req.Context(), req, "builds", "run-1"); err == nil {
		t.Fatal("request to an untrusted API server succeeded")
	}
	if called {
		t.Fatal("untrusted API server received the TokenReview")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}

func newKubeClient(t *testing.T, host string, caData []byte) kubernetes.Interface {
	t.Helper()
	client, err := kubernetes.NewForConfig(&rest.Config{
		Host: host,
		ContentConfig: rest.ContentConfig{
			AcceptContentTypes: "application/json",
			ContentType:        "application/json",
		},
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caData,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func certificatePEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}
