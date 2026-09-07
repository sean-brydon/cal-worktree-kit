package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsPageAvailableWhenStopped(t *testing.T) {
	r := httptest.NewRequest("GET", "http://test/__worktree/logs", nil)
	w := httptest.NewRecorder()
	serveLogs(w, r, "/tmp/routes/abcdef123456.json", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Running app") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("logs must not be cached")
	}
	if !strings.Contains(w.Body.String(), "textContent=") {
		t.Fatal("render log output as text")
	}
}
func TestLogsRejectCrossSiteAndInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		key, site string
		want      int
	}{{"../../secrets", "", 404}, {"abcdef123456", "cross-site", 403}} {
		r := httptest.NewRequest("GET", "http://test/__worktree/logs/data", nil)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		serveLogs(w, r, tc.key+".json", true)
		if w.Code != tc.want {
			t.Fatal(w.Code, tc.want)
		}
	}
}
func TestSetupTailIsBounded(t *testing.T) {
	base := t.TempDir()
	os.Mkdir(filepath.Join(base, "routes"), 0700)
	os.WriteFile(filepath.Join(base, "abcdef123456-setup.log"), []byte(strings.Repeat("x", 150*1024)+"END"), 0600)
	r := httptest.NewRequest("GET", "http://test/__worktree/logs/data", nil)
	w := httptest.NewRecorder()
	serveLogs(w, r, filepath.Join(base, "routes", "abcdef123456.json"), false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "END") || w.Body.Len() > 140*1024 {
		t.Fatal("bad log bounds", w.Code, w.Body.Len())
	}
}

func TestSharingControlsRequireLocalIngressAndExactOrigin(t *testing.T) {
	route := Route{Host: "branch.work.cal.localhost", Active: true, Shared: true, TailnetURL: "https://dev.example.ts.net:18443"}
	for _, tc := range []struct {
		local  bool
		origin string
	}{{false, "http://branch.work.cal.localhost"}, {true, "http://evil.work.cal.localhost"}, {true, ""}} {
		r := httptest.NewRequest("POST", "http://branch.work.cal.localhost/__worktree/share/data", strings.NewReader(`{"action":"off"}`))
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		serveShare(w, r, "/tmp/routes/abcdef123456.json", route, tc.local)
		if w.Code != 403 {
			t.Fatalf("unauthorized sharing mutation returned %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://branch.work.cal.localhost/__worktree/share/data", nil)
	w := httptest.NewRecorder()
	serveShare(w, r, "/tmp/routes/abcdef123456.json", route, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"canManage":false`) {
		t.Fatal(w.Body.String())
	}
}
func TestBranchCookiesCannotLeakAcrossPorts(t *testing.T) {
	r := httptest.NewRequest("GET", "https://dev.example.ts.net:18443/", nil)
	r.Header.Set("Cookie", "__Secure-wt-abcdef123456-next-auth.session-token.0=own; __Secure-wt-999999999999-next-auth.session-token=other; __Secure-next-auth.session-token=unscoped; theme=dark")
	isolateRequestCookies(r, "abcdef123456")
	if got := r.Header.Get("Cookie"); got != "__Secure-next-auth.session-token.0=own; theme=dark" {
		t.Fatal(got)
	}
	response := &http.Response{Header: make(http.Header)}
	response.Header.Add("Set-Cookie", "__Secure-next-auth.session-token.0=own; Path=/; Secure; HttpOnly; SameSite=None")
	response.Header.Add("Set-Cookie", "__Secure-next-auth.session-token.1=; Path=/; Secure; HttpOnly; Max-Age=0")
	isolateResponseCookies(response, "abcdef123456")
	cookies := response.Cookies()
	if len(cookies) != 2 || cookies[0].Name != "__Secure-wt-abcdef123456-next-auth.session-token.0" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[1].MaxAge != -1 {
		t.Fatal(cookies)
	}
}

func TestDevelopmentOriginTranslationIsScoped(t *testing.T) {
	r := httptest.NewRequest("GET", "https://dev.example.ts.net:18443/_next/webpack-hmr", nil)
	r.Header.Set("Origin", "https://dev.example.ts.net:18443")
	if !validDevOrigin(r) {
		t.Fatal("own origin rejected")
	}
	normalizeDevOrigin(r)
	if r.Header.Get("Origin") != "http://localhost" {
		t.Fatal("dev origin not translated")
	}
	r.Header.Set("Origin", "https://evil.example")
	if validDevOrigin(r) {
		t.Fatal("foreign origin accepted")
	}
	r.URL.Path = "/api/auth/callback/credentials"
	normalizeDevOrigin(r)
	if r.Header.Get("Origin") != "https://evil.example" {
		t.Fatal("authentication origin must not be translated")
	}
}
