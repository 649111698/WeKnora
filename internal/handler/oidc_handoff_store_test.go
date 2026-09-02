package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestOIDCHandoffStoreOneTimeConsume(t *testing.T) {
	code, err := storeOIDCHandoffPayload("payload-A")
	if err != nil {
		t.Fatalf("storeOIDCHandoffPayload failed: %v", err)
	}
	if len(code) != 64 || strings.ContainsAny(code, "+/=") {
		t.Fatalf("code = %q, want 64 hex chars without URL-unsafe characters", code)
	}

	payload, ok := consumeOIDCHandoffPayload(code)
	if !ok || payload != "payload-A" {
		t.Fatalf("first consume = (%q, %v), want (payload-A, true)", payload, ok)
	}
	// 一次性：第二次取必须失败
	if _, ok := consumeOIDCHandoffPayload(code); ok {
		t.Fatalf("second consume succeeded, want one-time semantics")
	}
	// 空码
	if _, ok := consumeOIDCHandoffPayload("   "); ok {
		t.Fatalf("blank code consumed, want false")
	}
}

func TestOIDCHandoffExpires(t *testing.T) {
	code, err := storeOIDCHandoffPayload("payload-exp")
	if err != nil {
		t.Fatalf("storeOIDCHandoffPayload failed: %v", err)
	}
	// 直接改写条目过期时间模拟超时
	oidcHandoffMu.Lock()
	entry := oidcHandoffStore[code]
	entry.expiresAt = time.Now().Add(-time.Second)
	oidcHandoffStore[code] = entry
	oidcHandoffMu.Unlock()

	if _, ok := consumeOIDCHandoffPayload(code); ok {
		t.Fatalf("expired code consumed, want false")
	}
	if _, exists := func() (string, bool) {
		oidcHandoffMu.Lock()
		defer oidcHandoffMu.Unlock()
		k, ok := "", false
		for k = range oidcHandoffStore {
			if k == code {
				ok = true
			}
		}
		return k, ok
	}(); exists {
		t.Fatalf("expired entry still in store after consume attempt")
	}
}

func TestOIDCHandoffLazyPurgesExpiredEntries(t *testing.T) {
	stale := "stale-code-value"
	oidcHandoffMu.Lock()
	oidcHandoffStore[stale] = oidcHandoffEntry{payload: "x", expiresAt: time.Now().Add(-time.Minute)}
	oidcHandoffMu.Unlock()

	if _, err := storeOIDCHandoffPayload("fresh"); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	oidcHandoffMu.Lock()
	_, stillThere := oidcHandoffStore[stale]
	oidcHandoffMu.Unlock()
	if stillThere {
		t.Fatalf("expired entry survived a store() pass, want lazy purge")
	}
}

func newOIDCHandoffRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &AuthHandler{}
	r.GET("/api/v1/auth/oidc/result", h.GetOIDCHandoffResult)
	return r
}

func TestGetOIDCHandoffResultEndpoint(t *testing.T) {
	router := newOIDCHandoffRouter()

	code, err := storeOIDCHandoffPayload("payload-endpoint")
	if err != nil {
		t.Fatalf("store failed: %v", err)
	}

	// 有效码：返回 payload
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/result?code="+code, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Payload string `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !body.Success || body.Data.Payload != "payload-endpoint" {
		t.Fatalf("body = %+v, want success with payload-endpoint", body)
	}

	// 同码第二次（已消费）→ 400
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/result?code="+code, nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("second use status = %d, want 400", w2.Code)
	}

	// 未知码 → 400
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/result?code=does-not-exist", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("unknown code status = %d, want 400", w3.Code)
	}
}

func TestOIDCCallbackFragmentCarriesShortCode(t *testing.T) {
	big := strings.Repeat("x", 3000)
	fragment, err := oidcCallbackFragment(big)
	if err != nil {
		t.Fatalf("oidcCallbackFragment failed: %v", err)
	}
	if !strings.HasPrefix(fragment, "#oidc_code=") {
		t.Fatalf("fragment = %q, want #oidc_code= prefix", fragment)
	}
	if len(fragment) > 100 {
		t.Fatalf("fragment length = %d, want a short URL even for a 3KB payload", len(fragment))
	}
	if strings.Contains(fragment, big) {
		t.Fatalf("fragment leaks the payload inline, want server-side handoff only")
	}
	// 码可换回原 payload
	code := strings.TrimPrefix(fragment, "#oidc_code=")
	got, ok := consumeOIDCHandoffPayload(code)
	if !ok || got != big {
		t.Fatalf("consume = (%d bytes, %v), want original payload", len(got), ok)
	}
}
