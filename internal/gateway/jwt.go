package gateway

import (
	"crypto"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ride-dispatch/engine/internal/domain"
)

// UserClaims defines custom claims derived from the identity token.
type UserClaims struct {
	Role   string `json:"role"`
	Region string `json:"region,omitempty"`
	jwt.RegisteredClaims
}

// JWTValidator validates incoming bearer tokens against public keys.
type JWTValidator struct {
	issuer      string
	audience    string
	allowedAlgs []string
	publicKey   crypto.PublicKey
}

func NewJWTValidator(issuer, audience string, allowedAlgs []string, pubKey crypto.PublicKey) *JWTValidator {
	if len(allowedAlgs) == 0 {
		allowedAlgs = []string{"RS256", "ES256"}
	}
	return &JWTValidator{
		issuer:      issuer,
		audience:    audience,
		allowedAlgs: allowedAlgs,
		publicKey:   pubKey,
	}
}

// ValidateToken parses and cryptographically validates the token.
func (v *JWTValidator) ValidateToken(tokenString string) (*UserClaims, error) {
	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods(v.allowedAlgs), // Strictly pin allowed algorithms
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(30 * time.Second), // Max 30s clock skew per AGENTS.md Section 7
	}

	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(t *jwt.Token) (any, error) {
		// Reject any symmetric algorithms or none
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
		}
		return v.publicKey, nil
	}, parserOptions...)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, domain.ErrExpiredToken
		}
		return nil, domain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, domain.ErrInvalidToken
	}

	if claims.Subject == "" {
		return nil, domain.ErrInvalidToken
	}

	return claims, nil
}
