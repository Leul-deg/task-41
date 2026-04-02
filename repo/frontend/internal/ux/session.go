package ux

import "strings"

func ScopedKey(base, userID string) string {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		uid = "anon"
	}
	return base + ":" + uid
}

func RequiresSession(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" || p == "/" || p == "/hiring/kiosk" || p == "/hiring/kiosk/qr" {
		return false
	}
	if strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/rpc/") {
		return false
	}
	return true
}
