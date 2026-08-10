package githost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitLabProvider_CreatePullRequest(t *testing.T) {
	var gotPath, gotToken, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"iid":7,"web_url":"https://gitlab.com/group/sub/repo/-/merge_requests/7"}`))
	}))
	defer srv.Close()

	// subgroup：ownerRepo = group/sub/repo，project id 应编码为 group%2Fsub%2Frepo。
	p := NewGitLabProvider(srv.URL, "gitlab.com", "group/sub/repo")
	pr, err := p.CreatePullRequest(context.Background(), CreatePRRequest{
		Title: "feat: x", Head: "feat", Base: "main", Token: "tkn",
	})
	require.NoError(t, err)
	require.Equal(t, &PR{Number: 7, URL: "https://gitlab.com/group/sub/repo/-/merge_requests/7"}, pr)
	require.Equal(t, "/projects/group%2Fsub%2Frepo/merge_requests", gotPath)
	require.Equal(t, "tkn", gotToken)
	require.Contains(t, gotBody, `"source_branch":"feat"`)
	require.Contains(t, gotBody, `"target_branch":"main"`)
}

func TestGitLabProvider_MergePullRequest(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewGitLabProvider(srv.URL, "gitlab.com", "org/repo")
	err := p.MergePullRequest(context.Background(), MergePRRequest{Number: 7, Token: "tkn"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "/projects/org%2Frepo/merge_requests/7/merge", gotPath)
}

func TestGitLabProvider_MergePullRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed) // 405 不可合并
		_, _ = w.Write([]byte(`{"message":"Method Not Allowed"}`))
	}))
	defer srv.Close()

	p := NewGitLabProvider(srv.URL, "gitlab.com", "org/repo")
	err := p.MergePullRequest(context.Background(), MergePRRequest{Number: 7, Token: "tkn"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 405")
}

func TestGitLabProvider_CreatePullRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	p := NewGitLabProvider(srv.URL, "gitlab.com", "org/repo")
	_, err := p.CreatePullRequest(context.Background(), CreatePRRequest{Title: "x", Head: "f", Base: "m", Token: "bad"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 401")
}
