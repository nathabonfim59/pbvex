package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// storageIDPattern matches exactly the branded form produced by GenerateStorageID:
// the literal "pbv_" prefix followed by 32 lowercase hex characters (UUIDv4 sans dashes).
// Enforcing the canonical form rejects traversal fragments and non-branded ids at the boundary.
var storageIDPattern = regexp.MustCompile(`^pbv_[0-9a-f]{32}$`)

// GenerateStorageID returns an opaque, non-path-leaking, branded storage identifier.
func GenerateStorageID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("storage id entropy: %w", err)
	}
	return "pbv_" + strings.ReplaceAll(id.String(), "-", ""), nil
}

// GenerateToken returns a random, unforgeable upload token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateAttempt returns a random nonce for a single upload attempt.
func GenerateAttempt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("attempt nonce entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 digest of a token string.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ValidateStorageID enforces the canonical branded storage id form ("pbv_" + 32 hex).
// It rejects empty, oversized, path-bearing, and non-branded ids without leaking paths.
func ValidateStorageID(id string) error {
	if id == "" {
		return fmt.Errorf("storage id is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("storage id is too long")
	}
	if !storageIDPattern.MatchString(id) {
		return fmt.Errorf("storage id is invalid")
	}
	return nil
}
