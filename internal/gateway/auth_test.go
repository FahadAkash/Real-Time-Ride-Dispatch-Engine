package gateway_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/gateway"
)

func TestJWTValidator(t *testing.T) {
	// Generate test RSA key pair
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}
	pubKey := &privKey.PublicKey

	issuer := "dispatch-auth"
	audience := "dispatch-engine"
	validator := gateway.NewJWTValidator(issuer, audience, []string{"RS256"}, pubKey)

	// 1. Valid token
	validClaims := &gateway.UserClaims{
		Role:   "rider",
		Region: "downtown",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "rider-101",
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		},
	}
	validTokenStr, err := jwt.NewWithClaims(jwt.SigningMethodRS256, validClaims).SignedString(privKey)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	claims, err := validator.ValidateToken(validTokenStr)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims.Subject != "rider-101" || claims.Role != "rider" {
		t.Errorf("claims mismatch: sub=%s, role=%s", claims.Subject, claims.Role)
	}

	// 2. Expired token
	expiredClaims := &gateway.UserClaims{
		Role: "rider",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "rider-101",
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)), // expired past leeway
		},
	}
	expiredTokenStr, _ := jwt.NewWithClaims(jwt.SigningMethodRS256, expiredClaims).SignedString(privKey)
	_, err = validator.ValidateToken(expiredTokenStr)
	if err == nil {
		t.Errorf("expected error on expired token, got nil")
	}

	// 3. Wrong audience
	wrongAudClaims := &gateway.UserClaims{
		Role: "rider",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "rider-101",
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{"wrong-aud"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}
	wrongAudTokenStr, _ := jwt.NewWithClaims(jwt.SigningMethodRS256, wrongAudClaims).SignedString(privKey)
	_, err = validator.ValidateToken(wrongAudTokenStr)
	if err == nil {
		t.Errorf("expected error on wrong audience, got nil")
	}

	// 4. Symmetric HS256 attempt (forbidden)
	hsTokenStr, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims).SignedString([]byte("some-secret"))
	_, err = validator.ValidateToken(hsTokenStr)
	if err == nil {
		t.Errorf("expected error rejecting HS256 token, got nil")
	}
}

func TestObjectLevelAuthorization(t *testing.T) {
	ride := &domain.Ride{
		RideID:   "ride-999",
		RiderID:  "rider-alice",
		DriverID: "driver-bob",
		Status:   domain.StatusPendingAccept,
	}

	// 1. Correct Rider owns the ride -> Allowed
	aliceClaims := &gateway.UserClaims{
		Role: "rider",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "rider-alice",
		},
	}
	if err := gateway.VerifyObjectOwnership(aliceClaims, ride); err != nil {
		t.Errorf("expected Alice to be authorized for her own ride, got: %v", err)
	}

	// 2. Different Rider -> Forbidden
	charlieClaims := &gateway.UserClaims{
		Role: "rider",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "rider-charlie",
		},
	}
	if err := gateway.VerifyObjectOwnership(charlieClaims, ride); err == nil {
		t.Errorf("expected Charlie (unauthorized rider) to be rejected, got nil")
	}

	// 3. Assigned Driver -> Allowed
	bobClaims := &gateway.UserClaims{
		Role: "driver",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "driver-bob",
		},
	}
	if err := gateway.VerifyObjectOwnership(bobClaims, ride); err != nil {
		t.Errorf("expected assigned Driver Bob to be authorized, got: %v", err)
	}

	// 4. Different Driver -> Forbidden
	daveClaims := &gateway.UserClaims{
		Role: "driver",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "driver-dave",
		},
	}
	if err := gateway.VerifyObjectOwnership(daveClaims, ride); err == nil {
		t.Errorf("expected unassigned Driver Dave to be rejected, got nil")
	}

	// 5. Admin -> Always Allowed
	adminClaims := &gateway.UserClaims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "operator-007",
		},
	}
	if err := gateway.VerifyObjectOwnership(adminClaims, ride); err != nil {
		t.Errorf("expected Admin to be authorized, got: %v", err)
	}
}

