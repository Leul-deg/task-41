package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

func TestProtectedPageRedirectsWithoutSession(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	cfg := appConfig{BackendURL: backend.URL, ClientKey: "k", ClientSecret: "s"}
	web := httptest.NewServer(buildMux(cfg))
	defer web.Close()

	resp, err := http.Get(web.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Request.URL.Path != "/" {
		t.Fatalf("expected redirect to /, got %s", resp.Request.URL.Path)
	}
}

func TestLoginSetsCookieAndAllowsDashboard(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1"}`))
		case "/api/auth/me":
			if r.Header.Get("Authorization") != "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"u1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	cfg := appConfig{BackendURL: backend.URL, ClientKey: "k", ClientSecret: "s"}
	web := httptest.NewServer(buildMux(cfg))
	defer web.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	payload, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass"})
	resp, err := client.Post(web.URL+"/rpc/login", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected login 200, got %d", resp.StatusCode)
	}

	dash, err := client.Get(web.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer dash.Body.Close()
	if dash.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard 200, got %d", dash.StatusCode)
	}
}
