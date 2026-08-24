package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
