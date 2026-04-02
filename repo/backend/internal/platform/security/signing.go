package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func PayloadHash(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

func ComputeSignature(secret, method, path, timestamp, nonce, payloadHash string) string {
	msg := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, path, timestamp, nonce, payloadHash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func SignatureValid(expected, got string) bool {
	return hmac.Equal([]byte(expected), []byte(got))
}
