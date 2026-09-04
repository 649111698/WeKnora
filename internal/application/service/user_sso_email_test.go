package service

import "testing"

// 邮箱式企微 userid 的合成邮箱必须可逆：@ 编码为 %40、% 编码为 %25，
// 还原后与原 userid 完全一致（早期版本把 @ 替换成 _ 导致消息被企微
// 静默丢弃，见 Zhengwei.Zhou@pexetech.com 案例）。
func TestSyntheticEmailRoundTrip(t *testing.T) {
	cases := []struct{ userid string }{
		{"Mountain"},
		{"LiMeiMei"},
		{"Zhengwei.Zhou@pexetech.com"},
		{"user%name"},
		{"a@b%40c"},
	}
	for _, c := range cases {
		email := ssoSyntheticEmailV2("wecom", c.userid)
		got, ok := WeComUserIDFromEmail(email)
		if !ok || got != c.userid {
			t.Errorf("round-trip failed: userid=%q email=%q got=%q ok=%v", c.userid, email, got, ok)
		}
	}
}

func TestSyntheticEmailLegacyFormat(t *testing.T) {
	// 旧账号（@ 被替换成 _）保持原样还原；新编码不影响旧数据读取。
	got, ok := WeComUserIDFromEmail("wecom_Zhengwei.Zhou_pexetech.com@wecom.sso.weknora.local")
	if !ok || got != "Zhengwei.Zhou_pexetech.com" {
		t.Errorf("legacy decode=%q ok=%v", got, ok)
	}
}

func TestSyntheticEmailV2EncodesAt(t *testing.T) {
	email := ssoSyntheticEmailV2("wecom", "Zhengwei.Zhou@pexetech.com")
	want := "wecom_Zhengwei.Zhou%40pexetech.com@wecom.sso.weknora.local"
	if email != want {
		t.Fatalf("email=%q want %q", email, want)
	}
}
