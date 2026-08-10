package githost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubProvider_CreatePullRequest(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/org/repo/pull/42"}`))
	}))
	defer srv.Close()

	p := NewGitHubProvider(srv.URL, "github.com", "org/repo")
	pr, err := p.CreatePullRequest(context.Background(), CreatePRRequest{
		Title: "feat: x", Head: "feat", Base: "main", Body: "desc", Token: "tkn",
	})
	require.NoError(t, err)
	require.Equal(t, &PR{Number: 42, URL: "https://github.com/org/repo/pull/42"}, pr)
	require.Equal(t, "/repos/org/repo/pulls", gotPath)
	require.Equal(t, "Bearer tkn", gotAuth)
	require.Contains(t, gotBody, `"head":"feat"`)
	require.Contains(t, gotBody, `"base":"main"`)
	require.Contains(t, gotBody, `"title":"feat: x"`)
}

func TestGitHubProvider_MergePullRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewGitHubProvider(srv.URL, "github.com", "org/repo")
	err := p.MergePullRequest(context.Background(), MergePRRequest{Number: 42, Token: "tkn"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "/repos/org/repo/pulls/42/merge", gotPath)
	require.Equal(t, "Bearer tkn", gotAuth)
}

func TestGitHubProvider_MergePullRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict) // 409 不可合并
		_, _ = w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
	}))
	defer srv.Close()

	p := NewGitHubProvider(srv.URL, "github.com", "org/repo")
	err := p.MergePullRequest(context.Background(), MergePRRequest{Number: 42, Token: "tkn"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 409")
}

func TestGitHubProvider_CreatePullRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	p := NewGitHubProvider(srv.URL, "github.com", "org/repo")
	_, err := p.CreatePullRequest(context.Background(), CreatePRRequest{Title: "x", Head: "f", Base: "m", Token: "t"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 422")
}
