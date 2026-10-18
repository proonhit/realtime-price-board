package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Tokens issues and verifies HS256-signed access tokens.
type Tokens struct {
	Secret []byte
	TTL    time.Duration
}

// IssueAccess creates an access token with sub = userID, plus iat and exp claims.
func (t Tokens) IssueAccess(userID string) (string, error) {
	if len(t.Secret) == 0 {
		return "", errors.New("auth: empty JWT secret")
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.TTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.Secret)
}

// ParseAccess verifies the token's signature and expiry and returns its subject.
// Only HS256 is accepted, so tokens using "none" or other algorithms are rejected.
func (t Tokens) ParseAccess(token string) (string, error) {
	if len(t.Secret) == 0 {
		return "", errors.New("auth: empty JWT secret")
	}
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return t.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", errors.New("auth: token has no subject")
	}
	return claims.Subject, nil
}

// NewRefreshToken returns a random 32-byte token encoded as base64url, and its
// SHA-256 hex hash. Give raw to the client; store only hash.
func NewRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken returns the SHA-256 hex hash of a raw refresh token.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
