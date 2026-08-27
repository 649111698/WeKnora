package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestKingdeeEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.TenantSSOConfig
		want bool
	}{
		{"nil config", nil, false},
		{"nil kingdee", &types.TenantSSOConfig{}, false},
		{"missing base url", &types.TenantSSOConfig{Kingdee: &types.KingdeeTenantSSO{AppClientID: "sys01"}}, false},
		{"missing client id", &types.TenantSSOConfig{Kingdee: &types.KingdeeTenantSSO{BaseURL: "https://kde.erp.com"}}, false},
		{"complete", &types.TenantSSOConfig{Kingdee: &types.KingdeeTenantSSO{BaseURL: "https://kde.erp.com", AppClientID: "sys01"}}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.KingdeeEnabled(); got != tc.want {
			t.Errorf("%s: KingdeeEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKingdeeSecretMaskAndMerge(t *testing.T) {
	cfg := &types.TenantSSOConfig{
		LoginDomain: "rag.example.com",
		Kingdee:     &types.KingdeeTenantSSO{BaseURL: "https://kde.erp.com", AppClientID: "sys01", AppSecret: "s3cret"},
	}
	masked := types.TenantSSOConfigForResponse(cfg)
	if masked.Kingdee.AppSecret != "***" {
		t.Fatalf("masked AppSecret = %q, want ***", masked.Kingdee.AppSecret)
	}

	// 提交掩码或空 secret 均保留原值；其它平台配置不受影响。
	merged := types.MergeTenantSSOConfigForUpdate(
		&types.TenantSSOConfig{Kingdee: &types.KingdeeTenantSSO{BaseURL: "https://kde2.erp.com", AppClientID: "sys02", AppSecret: "***"}},
		cfg,
	)
	if merged.Kingdee.AppSecret != "s3cret" {
		t.Fatalf("mask merge AppSecret = %q, want original", merged.Kingdee.AppSecret)
	}
	if merged.Kingdee.BaseURL != "https://kde2.erp.com" || merged.Kingdee.AppClientID != "sys02" {
		t.Fatalf("non-secret fields should follow submission, got %+v", merged.Kingdee)
	}

	mergedEmpty := types.MergeTenantSSOConfigForUpdate(
		&types.TenantSSOConfig{Kingdee: &types.KingdeeTenantSSO{BaseURL: "https://kde.erp.com", AppClientID: "sys01", AppSecret: ""}},
		cfg,
	)
	if mergedEmpty.Kingdee.AppSecret != "s3cret" {
		t.Fatalf("empty-secret merge should keep original, got %q", mergedEmpty.Kingdee.AppSecret)
	}
}

func TestGetKingdeeIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ierp/kapi/v2/secm/authen/getUserInfo" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("code") != "the-code" {
			t.Errorf("unexpected code %q", r.URL.Query().Get("code"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{
				"email": "zhangsan@corp.com", "mobile": "13800000000",
				"name": "张三", "userName": "zhangsan",
			},
			"errorCode": "0",
			"status":    true,
		})
	}))
	defer srv.Close()

	svc := &userService{}
	id, name, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{BaseURL: srv.URL + "/"}, "the-code")
	if err != nil {
		t.Fatalf("getKingdeeIdentity: %v", err)
	}
	if id != "zhangsan" || name != "张三" {
		t.Fatalf("identity = %q/%q, want zhangsan/张三", id, name)
	}
}

func TestGetKingdeeIdentityRejectsFailureAndMissingUser(t *testing.T) {
	svc := &userService{}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{}, "errorCode": "KDS_0001", "message": "code expired", "status": false,
		})
	}))
	defer failSrv.Close()
	if _, _, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{BaseURL: failSrv.URL}, "c"); err == nil {
		t.Fatal("expected error for failed status")
	}

	noUserSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"name": "无名"}, "errorCode": "0", "status": true,
		})
	}))
	defer noUserSrv.Close()
	if _, _, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{BaseURL: noUserSrv.URL}, "c"); err == nil {
		t.Fatal("expected error when userName missing")
	}
}

// 部分私有化网关把 ierp 应用直接挂在上下文根，kapi 没有 /ierp 前缀；
// 指南路径 404 时应自动退到无前缀路径。
func TestGetKingdeeIdentityFallsBackToContextRootKapi(t *testing.T) {
	var hitPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPaths = append(hitPaths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/ierp/kapi/v2/secm/authen/getUserInfo") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/kapi/v2/secm/authen/getUserInfo") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data":      map[string]string{"name": "李四", "userName": "lisi"},
			"errorCode": "0",
			"status":    true,
		})
	}))
	defer srv.Close()

	svc := &userService{}
	id, name, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{BaseURL: srv.URL + "/small/"}, "c")
	if err != nil {
		t.Fatalf("getKingdeeIdentity fallback: %v", err)
	}
	if id != "lisi" || name != "李四" {
		t.Fatalf("identity = %q/%q, want lisi/李四", id, name)
	}
	if len(hitPaths) != 2 ||
		!strings.HasSuffix(hitPaths[0], "/ierp/kapi/v2/secm/authen/getUserInfo") ||
		!strings.HasSuffix(hitPaths[1], "/kapi/v2/secm/authen/getUserInfo") {
		t.Fatalf("expected 404 fallback probe order, got %v", hitPaths)
	}
}

// 两个候选路径都不存在时，应返回 404 错误而不是静默成功。
func TestGetKingdeeIdentityBothPathsMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := &userService{}
	if _, _, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{BaseURL: srv.URL}, "c"); err == nil {
		t.Fatal("expected error when both kapi paths are missing")
	}
}

// token 模式：先 POST /kapi/oauth2/getToken（官方 JSON 契约，含 nonce/
// timestamp/language）换 access_token，getUserInfo 通过字面量名为
// access_token 的请求头携带。
func TestGetKingdeeIdentityTokenMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/kapi/oauth2/getToken":
			if r.Method != http.MethodPost {
				t.Errorf("token endpoint method = %s, want POST", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("token endpoint Content-Type = %q, want application/json", ct)
			}
			var body struct {
				ClientID     string `json:"client_id"`
				ClientSecret string `json:"client_secret"`
				Username     string `json:"username"`
				AccountID    string `json:"accountId"`
				Nonce        string `json:"nonce"`
				Timestamp    string `json:"timestamp"`
				Language     string `json:"language"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode token body: %v", err)
				return
			}
			if body.ClientSecret != "s3cret" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"description": "secret invalid", "errorcode": "login.loginBizException",
				})
				return
			}
			for _, kv := range [][2]string{
				{"client_id", "sys01"}, {"username", "proxyuser"},
				{"accountId", "1001"}, {"language", "zh_CN"},
			} {
				// 结构体字段名与 JSON tag 对应
				var got string
				switch kv[0] {
				case "client_id":
					got = body.ClientID
				case "username":
					got = body.Username
				case "accountId":
					got = body.AccountID
				case "language":
					got = body.Language
				}
				if got != kv[1] {
					t.Errorf("token body %s = %q, want %q", kv[0], got, kv[1])
				}
			}
			if body.Nonce == "" || body.Timestamp == "" {
				t.Errorf("nonce/timestamp must be non-empty, got %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				// expires_in 用毫秒形态验证换算
				"data":      map[string]interface{}{"access_token": "tok123", "expires_in": 7199992},
				"errorCode": "0", "status": true,
			})
		case "/kapi/v2/secm/authen/getUserInfo":
			if got := r.Header.Get("access_token"); got != "tok123" {
				t.Errorf("getUserInfo access_token header = %q, want tok123", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data":      map[string]string{"name": "王五", "userName": "wangwu"},
				"errorCode": "0",
				"status":    true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := &userService{}
	id, name, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{
		BaseURL: srv.URL, AppClientID: "sys01", AppSecret: "s3cret",
		ProxyUsername: "proxyuser", AccountID: "1001",
	}, "the-code")
	if err != nil {
		t.Fatalf("getKingdeeIdentity token mode: %v", err)
	}
	if id != "wangwu" || name != "王五" {
		t.Fatalf("identity = %q/%q, want wangwu/王五", id, name)
	}

	// token 端点报错时应把错误带出来
	if _, _, err := svc.getKingdeeIdentity(context.Background(), &types.KingdeeTenantSSO{
		BaseURL: srv.URL, AppClientID: "sys01", AppSecret: "bad",
		ProxyUsername: "proxyuser", AccountID: "1001",
	}, "the-code"); err == nil {
		t.Fatal("expected error when credentials rejected")
	}
}
