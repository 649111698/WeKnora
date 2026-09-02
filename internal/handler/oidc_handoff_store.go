package handler

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

/*
 * 登录回调结果的一次性服务端暂存。
 *
 * 企微/飞书/OIDC 回调成功后，后端需要把登录 payload（token、用户、租户
 * 等，约 2-3KB）交回前端 SPA。旧方式是 302 到 /#oidc_result=<payload>，
 * 但 iOS 企微内置浏览器加载带多 KB hash 的页面时不执行页面 JS（实测
 * 零请求白屏），SPA 无法消费回调。改为：后端把 payload 暂存在内存里并
 * 302 到 /#oidc_code=<64位随机码>，SPA 启动后用该码调
 * GET /auth/oidc/result 换回 payload——落地 URL 只有几十字节。
 *
 * 码为 256 位随机数、2 分钟过期、取一次即焚，无需额外鉴权。
 */

// oidcHandoffTTL 覆盖「302 → SPA 启动 → 发起换取」的窗口，同时保证
// code 泄露后的可利用时间足够短。
const oidcHandoffTTL = 2 * time.Minute

type oidcHandoffEntry struct {
	payload   string
	expiresAt time.Time
}

var (
	oidcHandoffMu    sync.Mutex
	oidcHandoffStore = map[string]oidcHandoffEntry{}
)

// storeOIDCHandoffPayload 暂存登录 payload，返回一次性换取码。
func storeOIDCHandoffPayload(payload string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	code := hex.EncodeToString(buf)
	now := time.Now()
	oidcHandoffMu.Lock()
	defer oidcHandoffMu.Unlock()
	// 惰性清理过期条目，避免未消费的回调码堆积。
	for k, v := range oidcHandoffStore {
		if now.After(v.expiresAt) {
			delete(oidcHandoffStore, k)
		}
	}
	oidcHandoffStore[code] = oidcHandoffEntry{payload: payload, expiresAt: now.Add(oidcHandoffTTL)}
	return code, nil
}

// consumeOIDCHandoffPayload 用换取码取回 payload；一次性消费，不存在或
// 已过期/已用过都返回 false。
func consumeOIDCHandoffPayload(code string) (string, bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", false
	}
	oidcHandoffMu.Lock()
	defer oidcHandoffMu.Unlock()
	entry, ok := oidcHandoffStore[code]
	if !ok {
		return "", false
	}
	delete(oidcHandoffStore, code)
	if time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.payload, true
}
