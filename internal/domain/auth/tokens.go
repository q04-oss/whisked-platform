package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// issueTokenPair creates a new access + refresh token pair for the given customer.
// The refresh token JTI is returned separately so it can be stored in Redis.
func issueTokenPair(customerID platform.CustomerID, secret string) (*TokenPair, string, error) {
	accessJTI := uuid.NewString()
	refreshJTI := uuid.NewString()
	now := time.Now()

	accessToken, err := signToken(jwt.MapClaims{
		"sub": customerID.Int64(),
		"jti": accessJTI,
		"iat": now.Unix(),
		"exp": now.Add(accessTokenTTL).Unix(),
	}, secret)
	if err != nil {
		return nil, "", fmt.Errorf("signing access token: %w", err)
	}

	refreshToken, err := signToken(jwt.MapClaims{
		"sub":  customerID.Int64(),
		"jti":  refreshJTI,
		"type": "refresh",
		"iat":  now.Unix(),
		"exp":  now.Add(refreshTokenTTL).Unix(),
	}, secret)
	if err != nil {
		return nil, "", fmt.Errorf("signing refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	}, refreshJTI, nil
}

// parseToken validates a JWT and returns its claims.
func parseToken(tokenStr, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

func signToken(claims jwt.MapClaims, secret string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// customerIDFromClaims extracts and validates the CustomerID from JWT claims.
func customerIDFromClaims(claims jwt.MapClaims) (platform.CustomerID, error) {
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid sub claim")
	}
	return platform.CustomerID(int64(sub)), nil
}

// jtiFromClaims extracts the JWT ID from claims.
func jtiFromClaims(claims jwt.MapClaims) (string, error) {
	jti, ok := claims["jti"].(string)
	if !ok || jti == "" {
		return "", fmt.Errorf("missing jti claim")
	}
	return jti, nil
}
