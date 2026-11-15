package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"skillshare/internal/install"
	"skillshare/internal/plainhub"
)

type plainHubFetcher struct{}

func (plainHubFetcher) Head(*install.Source, install.ParseOptions) (string, error) {
	return "abc1234", nil
}

func (plainHubFetcher) Discover(*install.Source) ([]install.SkillInfo, error) {
	return []install.SkillInfo{{Name: "a", Path: "skills/a"}, {Name: "b", Path: "skills/b"}}, nil
}

func getPlainHubInfo(t *testing.T, s *Server, hub string) plainHubInfo {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/fork/plainhub?hub="+url.QueryEscape(hub), nil)
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var info plainHubInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	return info
}

func TestHandleForkPlainHub(t *testing.T) {
	s, _ := newTestServer(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if info := getPlainHubInfo(t, s, "https://raw.githubusercontent.com/runkids/skillshare-hub/main/skillshare-hub.json"); info.Plain {
		t.Errorf("index file reported as a repo: %+v", info)
	}
	if info := getPlainHubInfo(t, s, "anthropics/skills"); !info.Plain || info.Commit != "" {
		t.Errorf("before a load: %+v", info)
	}

	l := plainhub.Default()
	l.Fetcher = plainHubFetcher{}
	l.Limit = 1
	if _, err := l.Load("anthropics/skills", install.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	info := getPlainHubInfo(t, s, "https://github.com/anthropics/skills")
	if !info.Plain || info.Repo != "anthropics/skills" || info.Commit != "abc1234" || info.IndexedAt == nil ||
		info.Listed != 1 || info.Total != 2 || !info.Capped {
		t.Errorf("after a load: %+v", info)
	}
}
