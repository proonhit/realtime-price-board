package auth

import (
	"testing"
	"time"
)

func TestIssueAndParseAccess(t *testing.T) {
	tokens := Tokens{Secret: []byte("test-secret"), TTL: time.Minute}
	token, err := tokens.IssueAccess("user-42")
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	userID, err := tokens.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if userID != "user-42" {
		t.Fatalf("expected user-42, got %q", userID)
	}
}

func TestParseAccessExpired(t *testing.T) {
	tokens := Tokens{Secret: []byte("test-secret"), TTL: -time.Minute}
	token, err := tokens.IssueAccess("user-42")
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if _, err := tokens.ParseAccess(token); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestNewRefreshToken(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if len(raw) != 43 { // 32 bytes in unpadded base64url
		t.Fatalf("expected raw length 43, got %d", len(raw))
	}
	if len(hash) != 64 { // SHA-256 in hex
		t.Fatalf("expected hash length 64, got %d", len(hash))
	}
	if hash != HashRefreshToken(raw) {
		t.Fatal("hash does not match HashRefreshToken(raw)")
	}
	raw2, _, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	if raw == raw2 {
		t.Fatal("expected two calls to produce different tokens")
	}
}

func TestParseAccessWrongSecret(t *testing.T) {
	signer := Tokens{Secret: []byte("secret-a"), TTL: time.Minute}
	verifier := Tokens{Secret: []byte("secret-b"), TTL: time.Minute}
	token, err := signer.IssueAccess("user-42")
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if _, err := verifier.ParseAccess(token); err == nil {
		t.Fatal("expected error for token signed with another secret")
	}
}
