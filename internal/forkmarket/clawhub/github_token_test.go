package clawhub

import "testing"

func TestGitHubAPIHeadersSendTheTokenOnlyToGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	if got := githubAPIHeaders(DefaultGitHubAPI)["Authorization"]; got != "Bearer test-token" {
		t.Fatalf("api.github.com: Authorization = %q, want the token", got)
	}
	if got, ok := githubAPIHeaders("http://127.0.0.1:8080")["Authorization"]; ok {
		t.Fatalf("another host got Authorization %q", got)
	}
	if githubAPIHeaders(DefaultGitHubAPI)["X-GitHub-Api-Version"] == "" {
		t.Fatal("API version header missing")
	}
}
