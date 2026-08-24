package handler

import "testing"

func TestSanitizeLoginTarget(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"/platform/creatChat", "/platform/creatChat"},
		{" /platform/creatChat ", "/platform/creatChat"},
		{"//evil.example.com", ""},     // 协议相对外站
		{"https://evil.example.com", ""}, // 绝对外站
		{"platform/creatChat", ""},     // 非路径
		{"javascript:alert(1)", ""},    // 非路径
	}
	for _, tc := range cases {
		if got := sanitizeLoginTarget(tc.in); got != tc.want {
			t.Errorf("sanitizeLoginTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
