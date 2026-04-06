package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/skip2/go-qrcode"
	"meridian/frontend/internal/ux"
	"meridian/frontend/ui/templates"
)

const (
	accessCookieName  = "meridian_access"
	refreshCookieName = "meridian_refresh"
)

type appConfig struct {
	Port         string
	BackendURL   string
	ClientKey    string
	ClientSecret string
	KioskSecret  string
	CookieSecure bool
}

func main() {
	cfg := appConfig{
		Port:         env("FRONTEND_PORT", "8081"),
		BackendURL:   strings.TrimRight(env("BACKEND_BASE_URL", "http://localhost:8080"), "/"),
		ClientKey:    env("H5_CLIENT_KEY", "local-h5"),
		ClientSecret: env("H5_CLIENT_SECRET", "local-h5-secret-change-me"),
		KioskSecret:  env("H5_KIOSK_SUBMIT_SECRET", "local-kiosk-submit-secret-change-me"),
		CookieSecure: envBool("FRONTEND_COOKIE_SECURE", false),
	}

	addr := ":" + cfg.Port
	log.Printf("frontend-web listening on %s", addr)
	if err := http.ListenAndServe(addr, buildMux(cfg)); err != nil {
		log.Fatalf("frontend failed: %v", err)
	}
}

func buildMux(cfg appConfig) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("ui/static"))))

	mux.Handle("/", templ.Handler(templates.LoginPage()))
	mux.Handle("/hiring/kiosk", templ.Handler(templates.HiringKioskPage()))
	mux.Handle("/dashboard", requireSessionHTML(templ.Handler(templates.DashboardPage()), cfg))
	mux.Handle("/dashboard/", requireSessionHTML(templ.Handler(templates.DashboardPage()), cfg))
	mux.Handle("/hiring", requireSessionHTML(templ.Handler(templates.HiringPage()), cfg))
	mux.Handle("/hiring/", requireSessionHTML(templ.Handler(templates.HiringPage()), cfg))
	mux.Handle("/support", requireSessionHTML(templ.Handler(templates.SupportPage()), cfg))
	mux.Handle("/support/", requireSessionHTML(templ.Handler(templates.SupportPage()), cfg))
	mux.Handle("/inventory", requireSessionHTML(templ.Handler(templates.InventoryPage()), cfg))
	mux.Handle("/inventory/", requireSessionHTML(templ.Handler(templates.InventoryPage()), cfg))
	mux.Handle("/compliance", requireSessionHTML(templ.Handler(templates.CompliancePage()), cfg))
	mux.Handle("/compliance/", requireSessionHTML(templ.Handler(templates.CompliancePage()), cfg))

	mux.HandleFunc("/hiring/kiosk/qr", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		if strings.TrimSpace(u) == "" {
			u = "http://localhost:" + cfg.Port + "/hiring/kiosk"
		}
		if _, err := url.ParseRequestURI(u); err != nil {
			http.Error(w, "invalid kiosk url", http.StatusBadRequest)
			return
		}
		png, err := qrcode.Encode(u, qrcode.Medium, 256)
		if err != nil {
			http.Error(w, "failed to generate qr", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(png)
	})

	mux.HandleFunc("/rpc/login", func(w http.ResponseWriter, r *http.Request) {
		proxyAuthJSON(w, r, cfg, cfg.BackendURL+"/auth/login")
	})
	mux.HandleFunc("/rpc/refresh", func(w http.ResponseWriter, r *http.Request) {
		proxyAuthJSON(w, r, cfg, cfg.BackendURL+"/auth/refresh")
	})
	mux.HandleFunc("/rpc/logout", func(w http.ResponseWriter, r *http.Request) {
		clearAuthCookies(w, cfg)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/rpc/api/", func(w http.ResponseWriter, r *http.Request) {
		targetPath := strings.TrimPrefix(r.URL.Path, "/rpc")
		targetURL := cfg.BackendURL + targetPath
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}
		proxySigned(w, r, targetURL, cfg)
	})
	mux.HandleFunc("/rpc/kiosk/applications", func(w http.ResponseWriter, r *http.Request) {
		proxySignedWithKiosk(w, r, cfg.BackendURL+"/kiosk/applications", cfg)
	})
	mux.HandleFunc("/rpc/kiosk/jobs", func(w http.ResponseWriter, r *http.Request) {
		proxySignedWithKiosk(w, r, cfg.BackendURL+"/kiosk/jobs", cfg)
	})

	return mux
}

func requireSessionHTML(next http.Handler, cfg appConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ux.RequiresSession(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		accessToken, _ := getCookieValue(r, accessCookieName)
		if accessToken == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		if validateAccessToken(r.Context(), cfg, accessToken) {
			next.ServeHTTP(w, r)
			return
		}

		refreshToken, _ := getCookieValue(r, refreshCookieName)
		if refreshToken != "" {
			if refreshed, _, ok := refreshTokens(r.Context(), cfg, refreshToken); ok {
				setAuthCookies(w, cfg, refreshed, refreshToken)
				if validateAccessToken(r.Context(), cfg, refreshed) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		clearAuthCookies(w, cfg)
		http.Redirect(w, r, "/", http.StatusFound)
	})
}

func proxyAuthJSON(w http.ResponseWriter, r *http.Request, cfg appConfig, target string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.HasSuffix(target, "/auth/refresh") {
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload == nil {
			payload = map[string]any{}
		}
		if strings.TrimSpace(fmt.Sprint(payload["refresh_token"])) == "" {
			if rt, ok := getCookieValue(r, refreshCookieName); ok {
				payload["refresh_token"] = rt
				rebuilt, _ := json.Marshal(payload)
				body = rebuilt
			}
		}
	}

	req, err := signedBackendRequest(r.Context(), r.Method, target, body, cfg)
	if err != nil {
		http.Error(w, "proxy request creation failed", http.StatusInternalServerError)
		return
	}
	copyAuthHeaders(r, req)
	if req.Header.Get("Authorization") == "" {
		if token, ok := getCookieValue(r, accessCookieName); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "backend unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) == nil {
			access := strings.TrimSpace(fmt.Sprint(parsed["access_token"]))
			refresh := strings.TrimSpace(fmt.Sprint(parsed["refresh_token"]))
			if access != "" {
				setAuthCookies(w, cfg, access, refresh)
			}
		}
	}

	for k, vals := range resp.Header {
		if strings.EqualFold(k, "set-cookie") {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
}

func proxySigned(w http.ResponseWriter, r *http.Request, target string, cfg appConfig) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "proxy request creation failed", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	pHash := payloadHash(body)
	sig := computeSignature(cfg.ClientSecret, r.Method, req.URL.Path, timestamp, nonce, pHash)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Key", cfg.ClientKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", sig)
	copyAuthHeaders(r, req)
	if req.Header.Get("Authorization") == "" {
		if token, ok := getCookieValue(r, accessCookieName); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	if key := r.Header.Get("Idempotency-Key"); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if stepUp := r.Header.Get("X-Step-Up-Token"); stepUp != "" {
		req.Header.Set("X-Step-Up-Token", stepUp)
	}

	forward(w, req)
}

func proxySignedWithKiosk(w http.ResponseWriter, r *http.Request, target string, cfg appConfig) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "proxy request creation failed", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	pHash := payloadHash(body)
	sig := computeSignature(cfg.ClientSecret, r.Method, req.URL.Path, timestamp, nonce, pHash)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Key", cfg.ClientKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Kiosk-Token", cfg.KioskSecret)

	forward(w, req)
}

func validateAccessToken(ctx context.Context, cfg appConfig, accessToken string) bool {
	req, err := signedBackendRequest(ctx, http.MethodGet, cfg.BackendURL+"/api/auth/me", nil, cfg)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func refreshTokens(ctx context.Context, cfg appConfig, refreshToken string) (string, string, bool) {
	payload := map[string]string{"refresh_token": refreshToken}
	body, _ := json.Marshal(payload)
	req, err := signedBackendRequest(ctx, http.MethodPost, cfg.BackendURL+"/auth/refresh", body, cfg)
	if err != nil {
		return "", "", false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", false
	}

	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return "", "", false
	}
	access := strings.TrimSpace(fmt.Sprint(parsed["access_token"]))
	refresh := strings.TrimSpace(fmt.Sprint(parsed["refresh_token"]))
	if access == "" {
		return "", "", false
	}
	return access, refresh, true
}

func signedBackendRequest(ctx context.Context, method, target string, body []byte, cfg appConfig) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	pHash := payloadHash(body)
	sig := computeSignature(cfg.ClientSecret, method, req.URL.Path, timestamp, nonce, pHash)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Key", cfg.ClientKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", sig)
	return req, nil
}

func setAuthCookies(w http.ResponseWriter, cfg appConfig, access, refresh string) {
	if strings.TrimSpace(access) != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     accessCookieName,
			Value:    access,
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   15 * 60,
		})
	}
	if strings.TrimSpace(refresh) != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     refreshCookieName,
			Value:    refresh,
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   7 * 24 * 60 * 60,
		})
	}
}

func clearAuthCookies(w http.ResponseWriter, cfg appConfig) {
	http.SetCookie(w, &http.Cookie{Name: accessCookieName, Path: "/", HttpOnly: true, Secure: cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName, Path: "/", HttpOnly: true, Secure: cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func getCookieValue(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return "", false
	}
	return c.Value, true
}

func copyAuthHeaders(src *http.Request, dst *http.Request) {
	if auth := src.Header.Get("Authorization"); auth != "" {
		dst.Header.Set("Authorization", auth)
	}
}

func forward(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "backend unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func payloadHash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func computeSignature(secret, method, path, timestamp, nonce, payloadHash string) string {
	msg := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, path, timestamp, nonce, payloadHash)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes"
}
