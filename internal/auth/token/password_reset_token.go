package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func GeneratePasswordResetCode() (string, error) {
	bytes := make([]byte, 4)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	number := uint32(bytes[0])<<24 |
		uint32(bytes[1])<<16 |
		uint32(bytes[2])<<8 |
		uint32(bytes[3])

	code := number % 1000000

	return fmt.Sprintf("%06d", code), nil
}

func HashPasswordResetCode(code string) string {
	hash := sha256.Sum256([]byte(code))

	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func GeneratePasswordResetToken() (string, error) {
	bytes := make([]byte, 32)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func HashPasswordResetToken(token string) string {
	hash := sha256.Sum256([]byte(token))

	return base64.RawURLEncoding.EncodeToString(hash[:])
}
