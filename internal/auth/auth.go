package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return hash, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {
	result, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}
	return result, nil
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	now := time.Now().UTC()
	issuedAt := jwt.NewNumericDate(now)
	expiresAt := jwt.NewNumericDate(now.Add(expiresIn))
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "chirpy-access", IssuedAt: issuedAt, ExpiresAt: expiresAt, Subject: userID.String()})

	signedToken, err := token.SignedString([]byte(tokenSecret))

	if err != nil {
		return "", err
	}

	return signedToken, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) { return []byte(tokenSecret), nil })
	if err != nil {
		return uuid.Nil, err
	}

	uuidStr, err := claims.GetSubject()
	if err != nil {
		return uuid.Nil, err
	}

	parsedUUID, err := uuid.Parse(uuidStr)
	if err != nil {
		return uuid.Nil, err
	}

	return parsedUUID, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	headerAuth := headers.Get("Authorization")

	if headerAuth == "" {
		return "", errors.New("authorization header is missing")
	}

	fields := strings.SplitN(headerAuth, " ", 2)

	if !strings.EqualFold(fields[0], "bearer") {
		return "", errors.New("authorization header doesn't follow bearer scheme")
	}

	if len(fields) < 2 {
		return "", errors.New("tokenstring doesn't exist")
	}

	token := strings.TrimSpace(fields[1])

	if token == "" {
		return "", errors.New("tokenstring is empty")
	}
	return token, nil
}
