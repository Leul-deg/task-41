package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenManager struct {
	secret []byte
}

func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{secret: []byte(secret)}
}

func (tm *TokenManager) CreateAccessToken(userID, username string, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub":      userID,
		"username": username,
		"kind":     "access",
		"exp":      time.Now().Add(ttl).Unix(),
		"iat":      time.Now().Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(tm.secret)
}

func (tm *TokenManager) CreateStepUpToken(userID, actionClass string, ttl time.Duration) (string, error) {
	nonce, err := NewOpaqueToken()
	if err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"sub":          userID,
		"kind":         "step_up",
		"action_class": actionClass,
		"exp":          time.Now().Add(ttl).Unix(),
		"iat":          time.Now().Unix(),
		"jti":          nonce,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(tm.secret)
}

func (tm *TokenManager) ParseToken(token string) (jwt.MapClaims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return tm.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

func NewOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
