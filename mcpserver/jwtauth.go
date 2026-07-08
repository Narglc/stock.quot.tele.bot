package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ctxKey 是本包在 request/handler context 里存 chat_id 的 key 类型。
type ctxKey string

const chatIDKey ctxKey = "mcp_chat_id"

// parseBearer 校验 Authorization: Bearer <jwt>（仅 HS256），返回 sub 里的 chat_id。
// 显式拒绝非 HMAC 签名算法（防 alg=none / RS-HS 混淆攻击）；exp 由 jwt v5 默认校验。
func parseBearer(secret, header string) (int64, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return 0, fmt.Errorf("missing bearer token")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if raw == "" {
		return 0, fmt.Errorf("empty bearer token")
	}

	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return 0, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return 0, fmt.Errorf("invalid token")
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return 0, fmt.Errorf("token missing sub")
	}
	chatID, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sub %q is not a chat_id: %w", sub, err)
	}
	return chatID, nil
}

// SignToken 用 HS256 + secret 签发一个 sub=chatID 的长期 JWT（不设过期，供自用长期 token）。
func SignToken(secret string, chatID int64) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("jwt secret 为空，无法签发（设置 MCP_JWT_SECRET）")
	}
	claims := jwt.MapClaims{"sub": strconv.FormatInt(chatID, 10)}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// withAuth 包一层 JWT 校验中间件：secret 为空则直接放行（回环免鉴权，向后兼容）；
// 非空则要求合法 Bearer JWT，校验通过后把 chat_id 注入 request context 供 handler 决定发送目标。
func withAuth(secret string, next http.Handler) http.Handler {
	if secret == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatID, err := parseBearer(secret, r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), chatIDKey, chatID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
