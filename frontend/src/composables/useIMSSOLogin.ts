/**
 * 企微/飞书内置浏览器 SSO 免登（JIT 自动建号）。
 *
 * 登录页挂载时调用 maybeStartIMSSOLogin()：
 * 1. UA 检测企微（wxwork）/飞书（feishu|Larksuite）内置浏览器；
 * 2. 未启用对应平台 SSO 则停留普通登录页；
 * 3. 命中且未带 code → 生成 state 存 sessionStorage，跳平台 OAuth 静默授权
 *    （企微 snsapi_base / 飞书 authen），redirect_uri 回到 /login；
 * 4. 回跳带 code → 校验 state 后转后端 /auth/sso/{platform}/callback，
 *    后端完成登录（首次自动建访客账号）后 302 回 /#oidc_result=...，
 *    由 App.vue 的 OIDC 回调链路统一持久化会话。
 *
 * SSO 凭证为租户级：登录链接 ?t=<tenant id>（或租户专属域名）定位租户，
 * t 参数会透传到 config 请求、平台授权 redirect_uri 与后端 callback；
 * 登录后直接进入该租户。无 t 时后端按 Host 匹配租户登录域名，
 * 全实例仅一个租户配置了 SSO 时自动回退到它。
 */

const SSO_STATE_KEY = 'im_sso_state'

export type IMSSOPlatform = 'wecom' | 'feishu'

export function detectIMSSOPlatform(): IMSSOPlatform | null {
  if (typeof navigator === 'undefined') return null
  const ua = navigator.userAgent
  if (/wxwork/i.test(ua)) return 'wecom'
  if (/feishu|larksuite/i.test(ua)) return 'feishu'
  return null
}

type SSOConfig = {
  wecom?: { enabled: boolean; corp_id?: string; agent_id?: string }
  feishu?: { enabled: boolean; app_id?: string }
}

// 当前登录链接携带的租户参数（?t=<tenant id>），全流程透传。
function ssoTenantParam(): string {
  return new URLSearchParams(window.location.search).get('t') || ''
}

async function fetchSSOConfig(): Promise<SSOConfig> {
  const t = ssoTenantParam()
  const qs = t ? `?t=${encodeURIComponent(t)}` : ''
  const res = await fetch(`/api/v1/auth/sso/config${qs}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) return {}
  return (await res.json()) as SSOConfig
}

function buildAuthorizeURL(platform: IMSSOPlatform, cfg: SSOConfig, state: string): string | null {
  const t = ssoTenantParam()
  const tenantQS = t ? `?t=${encodeURIComponent(t)}` : ''
  const redirectUri = encodeURIComponent(`${window.location.origin}/login${tenantQS}`)
  if (platform === 'wecom') {
    const wecom = cfg.wecom
    if (!wecom?.enabled || !wecom.corp_id) return null
    const agentid = wecom.agent_id ? `&agentid=${encodeURIComponent(wecom.agent_id)}` : ''
    return (
      'https://open.weixin.qq.com/connect/oauth2/authorize' +
      `?appid=${encodeURIComponent(wecom.corp_id)}` +
      `&redirect_uri=${redirectUri}` +
      '&response_type=code' +
      '&scope=snsapi_base' +
      `&state=${encodeURIComponent(state)}` +
      agentid +
      '#wechat_redirect'
    )
  }
  const feishu = cfg.feishu
  if (!feishu?.enabled || !feishu.app_id) return null
  return (
    'https://open.feishu.cn/open-apis/authen/v1/index' +
    `?app_id=${encodeURIComponent(feishu.app_id)}` +
    `&redirect_uri=${redirectUri}` +
    `&state=${encodeURIComponent(state)}`
  )
}

function randomState(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
}

export type IMSSOOutcome =
  | { action: 'none' } // 非企微/飞书环境或平台未启用，停留登录页
  | { action: 'redirecting' } // 正在跳平台授权或后端回调
  | { action: 'error'; message: string } // 状态校验失败等可展示错误

export async function maybeStartIMSSOLogin(): Promise<IMSSOOutcome> {
  const platform = detectIMSSOPlatform()
  if (!platform) return { action: 'none' }

  const params = new URLSearchParams(window.location.search)

  // 平台回跳：带 code，校验 state 后转后端换登录态
  const code = params.get('code')
  if (code) {
    const state = params.get('state') || ''
    const saved = sessionStorage.getItem(SSO_STATE_KEY)
    if (!saved || state !== saved) {
      sessionStorage.removeItem(SSO_STATE_KEY)
      return { action: 'error', message: 'SSO state 校验失败，请重新进入' }
    }
    sessionStorage.removeItem(SSO_STATE_KEY)
    const t = ssoTenantParam()
    const tenantQS = t ? `&t=${encodeURIComponent(t)}` : ''
    window.location.href =
      `/api/v1/auth/sso/${platform}/callback?code=${encodeURIComponent(code)}${tenantQS}`
    return { action: 'redirecting' }
  }

  // 后端失败回跳（#oidc_error 由 App.vue 统一展示，这里不重复处理）
  const cfg = await fetchSSOConfig()
  const state = randomState()
  const url = buildAuthorizeURL(platform, cfg, state)
  if (!url) return { action: 'none' }

  sessionStorage.setItem(SSO_STATE_KEY, state)
  window.location.href = url
  return { action: 'redirecting' }
}
