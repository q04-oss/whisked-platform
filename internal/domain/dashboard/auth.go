package dashboard

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

const (
	staffAccessTokenTTL  = 8 * time.Hour  // longer than customer tokens — dashboard sessions
	staffRefreshTokenTTL = 30 * 24 * time.Hour
	staffTokenType       = "staff"
)

// issueStaffTokenPair creates a JWT pair for a staff member.
// The `type: "staff"` claim distinguishes these from customer tokens —
// RequireStaffAuth middleware rejects any token without this claim.
func issueStaffTokenPair(staffID int64, role, secret string) (*StaffTokenPair, string, error) {
	now := time.Now()
	accessJTI := uuid.NewString()
	refreshJTI := uuid.NewString()

	accessToken, err := signStaffToken(jwt.MapClaims{
		"sub":  staffID,
		"jti":  accessJTI,
		"type": staffTokenType,
		"role": role,
		"iat":  now.Unix(),
		"exp":  now.Add(staffAccessTokenTTL).Unix(),
	}, secret)
	if err != nil {
		return nil, "", fmt.Errorf("signing staff access token: %w", err)
	}

	refreshToken, err := signStaffToken(jwt.MapClaims{
		"sub":  staffID,
		"jti":  refreshJTI,
		"type": staffTokenType + ":refresh",
		"role": role,
		"iat":  now.Unix(),
		"exp":  now.Add(staffRefreshTokenTTL).Unix(),
	}, secret)
	if err != nil {
		return nil, "", fmt.Errorf("signing staff refresh token: %w", err)
	}

	return &StaffTokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(staffAccessTokenTTL.Seconds()),
		Role:         role,
	}, refreshJTI, nil
}

func signStaffToken(claims jwt.MapClaims, secret string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// staffIDFromClaims extracts the staff ID from JWT claims.
func staffIDFromClaims(claims jwt.MapClaims) (platform.StaffID, error) {
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid sub claim")
	}
	return platform.StaffID(int64(sub)), nil
}

// staffRoleFromClaims extracts the role from JWT claims.
func staffRoleFromClaims(claims jwt.MapClaims) string {
	role, _ := claims["role"].(string)
	return role
}

// staffRefreshKey is the Redis key for a staff refresh token.
func staffRefreshKey(jti string) string { return "whisked:staff-refresh:" + jti }
