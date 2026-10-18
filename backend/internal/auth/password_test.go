package auth

import (
	"testing"
)

// Write function test for HashPassword and CheckPassword
func TestHashPassword(t *testing.T) {
	password := "password123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if hash == "" {
		t.Fatal("Expected hashed password, got empty string")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "password123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !CheckPassword(hash, password) {
		t.Fatal("Expected password to be valid")
	}
}
