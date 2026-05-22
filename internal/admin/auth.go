package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

type contextKey string

const userContextKey contextKey = "adminUser"

type tokenClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

func ensureInitialAdmin(db *gorm.DB, cfg config.AdminConfig) error {
	var count int64
	if err := db.Model(&database.AdminUser{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count admin users: %w", err)
	}
	if count > 0 {
		return nil
	}
	if strings.TrimSpace(cfg.InitialPassword) == "" {
		return errors.New("no admin user exists; set admin.initial_password or ADMIN_INITIAL_PASSWORD for first startup")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.InitialPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}
	username := strings.TrimSpace(cfg.InitialUsername)
	if username == "" {
		username = "admin"
	}
	user := &database.AdminUser{
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  username,
		Role:         "admin",
	}
	if err := db.Create(user).Error; err != nil {
		return fmt.Errorf("create initial admin user: %w", err)
	}
	return nil
}

func jwtSecret(cfg config.AdminConfig) ([]byte, error) {
	secret := strings.TrimSpace(cfg.JWTSecret)
	if secret != "" {
		return []byte(secret), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate ephemeral jwt secret: %w", err)
	}
	return buf, nil
}

func signToken(secret []byte, username string, ttl time.Duration) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := tokenClaims{Sub: username, Exp: time.Now().Add(ttl).Unix()}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyToken(secret []byte, token string) (tokenClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, false
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return tokenClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, false
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, false
	}
	if claims.Sub == "" || time.Now().Unix() > claims.Exp {
		return tokenClaims{}, false
	}
	return claims, true
}

func authenticate(db *gorm.DB, username, password string) (*database.AdminUser, error) {
	var user database.AdminUser
	if err := db.Where("username = ? AND disabled = ?", username, false).First(&user).Error; err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, err
	}
	return &user, nil
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func userFromContext(ctx context.Context) *database.AdminUser {
	user, _ := ctx.Value(userContextKey).(*database.AdminUser)
	return user
}
