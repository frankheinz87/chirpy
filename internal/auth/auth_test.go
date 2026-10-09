package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestHashPassword(t *testing.T) {
	// Each row checks that a different kind of input can be hashed. Argon2 uses
	// a random salt, so the test checks the hash's properties rather than an
	// exact expected string.
	tests := []struct {
		name     string
		password string
	}{
		{name: "ordinary password", password: "correct horse battery staple"},
		{name: "empty password", password: ""},
	}

	for _, test := range tests {
		// A subtest gives each input a separate result in the test runner.
		t.Run(test.name, func(t *testing.T) {
			hash, err := HashPassword(test.password)
			if err != nil {
				t.Fatalf("HashPassword() error = %v", err)
			}
			if hash == "" {
				t.Fatal("HashPassword() returned an empty hash")
			}
			if hash == test.password {
				t.Fatal("HashPassword() returned the plaintext password")
			}
			// A hash is useful only if the password checker can verify it.
			matched, err := CheckPasswordHash(test.password, hash)
			if err != nil {
				t.Fatalf("CheckPasswordHash() error = %v", err)
			}
			if !matched {
				t.Fatal("CheckPasswordHash() did not verify the generated hash")
			}
		})
	}
}

func TestCheckPasswordHash(t *testing.T) {
	// Hash once and reuse this fixture across cases. Creating an Argon2 hash is
	// intentionally expensive, while comparing against it covers all outcomes.
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	// A wrong password is a normal non-match (false, nil error). A malformed
	// hash cannot be checked, so those cases should return an error.
	tests := []struct {
		name      string
		password  string
		hash      string
		wantMatch bool
		wantErr   bool
	}{
		{name: "matching password", password: password, hash: hash, wantMatch: true},
		{name: "mismatching password", password: "not the password", hash: hash},
		{name: "malformed hash", password: password, hash: "not-a-password-hash", wantErr: true},
		{name: "empty hash", password: password, hash: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMatch, err := CheckPasswordHash(test.password, test.hash)
			// Compare whether an error occurred, not the library-specific text.
			if (err != nil) != test.wantErr {
				t.Fatalf("CheckPasswordHash() error = %v, wantErr %v", err, test.wantErr)
			}
			if gotMatch != test.wantMatch {
				t.Errorf("CheckPasswordHash() = %v, want %v", gotMatch, test.wantMatch)
			}
		})
	}
}

func TestMakeJWT(t *testing.T) {
	// Use more than one user and lifetime to check that the token carries the
	// requested identity and remains valid for its requested positive lifetime.
	tests := []struct {
		name      string
		userID    uuid.UUID
		expiresIn time.Duration
	}{
		{
			name:      "short-lived token",
			userID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
			expiresIn: time.Minute,
		},
		{
			name:      "long-lived token",
			userID:    uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"),
			expiresIn: 24 * time.Hour,
		},
	}
	const secret = "test-secret"

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := MakeJWT(test.userID, secret, test.expiresIn)
			if err != nil {
				t.Fatalf("MakeJWT() error = %v", err)
			}
			if token == "" {
				t.Fatal("MakeJWT() returned an empty token")
			}

			// Validate the generated token instead of comparing its exact text:
			// JWTs include timestamps and are signed, so their text is incidental.
			gotUserID, err := ValidateJWT(token, secret)
			if err != nil {
				t.Fatalf("ValidateJWT() error = %v", err)
			}
			if gotUserID != test.userID {
				t.Errorf("ValidateJWT() user ID = %v, want %v", gotUserID, test.userID)
			}
		})
	}
}

func TestValidateJWT(t *testing.T) {
	const secret = "test-secret"
	userID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	// These fixtures isolate validation stages: one valid token, one whose
	// expiration is already in the past, and one with a valid signature but an
	// invalid UUID subject.
	validToken, err := MakeJWT(userID, secret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() error = %v", err)
	}
	expiredToken, err := MakeJWT(userID, secret, -time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() for expired token error = %v", err)
	}

	// MakeJWT only accepts uuid.UUID, so create this token directly to exercise
	// ValidateJWT's subject-to-UUID parsing failure after signature validation.
	invalidSubjectToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		Subject:   "not-a-uuid",
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signing invalid-subject token: %v", err)
	}

	// Invalid tokens should return uuid.Nil and an error; valid tokens should
	// return their subject UUID and no error.
	tests := []struct {
		name       string
		token      string
		secret     string
		wantUserID uuid.UUID
		wantErr    bool
	}{
		{name: "valid token", token: validToken, secret: secret, wantUserID: userID},
		{name: "malformed token", token: "not.a.jwt", secret: secret, wantErr: true},
		{name: "wrong secret", token: validToken, secret: "wrong-secret", wantErr: true},
		{name: "expired token", token: expiredToken, secret: secret, wantErr: true},
		{name: "invalid UUID subject", token: invalidSubjectToken, secret: secret, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUserID, err := ValidateJWT(test.token, test.secret)
			// Do not depend on exact JWT-library error messages; only the
			// success/failure contract matters here.
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateJWT() error = %v, wantErr %v", err, test.wantErr)
			}
			if gotUserID != test.wantUserID {
				t.Errorf("ValidateJWT() user ID = %v, want %v", gotUserID, test.wantUserID)
			}
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		headers   http.Header
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			headers:   http.Header{"Authorization": []string{"Bearer abc123"}},
			wantToken: "abc123",
		},
		{
			name:      "case-insensitive bearer scheme",
			headers:   http.Header{"Authorization": []string{"bEaReR AbC123"}},
			wantToken: "AbC123",
		},
		{
			name:      "trim token whitespace",
			headers:   http.Header{"Authorization": []string{"Bearer   abc123  "}},
			wantToken: "abc123",
		},
		{
			name:    "missing authorization header",
			headers: make(http.Header),
			wantErr: true,
		},
		{
			name:    "empty authorization header",
			headers: http.Header{"Authorization": []string{""}},
			wantErr: true,
		},
		{
			name:    "wrong authorization scheme",
			headers: http.Header{"Authorization": []string{"Basic abc123"}},
			wantErr: true,
		},
		{
			name:    "missing token",
			headers: http.Header{"Authorization": []string{"Bearer"}},
			wantErr: true,
		},
		{
			name:    "empty token",
			headers: http.Header{"Authorization": []string{"Bearer   "}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotToken, err := GetBearerToken(test.headers)
			if (err != nil) != test.wantErr {
				t.Fatalf("GetBearerToken() error = %v, wantErr %v", err, test.wantErr)
			}
			if gotToken != test.wantToken {
				t.Errorf("GetBearerToken() token = %q, want %q", gotToken, test.wantToken)
			}
		})
	}
}
