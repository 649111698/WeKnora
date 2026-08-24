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
 * SSO 凭证为租户级，租户按访问域名区分：后端把请求 Host 与各租户
 * 配置的专属登录域名（login_domain）精确匹配，命中即用该租户的
 * 凭证发起免登，登录后直接进入该租户；未命中则停留普通登录页。
 */

const SSO_STATE_KEY = 'im_sso_state'
// 免登前用户想去的页面（?redirect=）。OAuth 往返 + 后端 302 回根路径都会
// 丢掉原 URL，用 sessionStorage 在同一标签页内把目标带到 App.vue 兑换处。
const SSO_REDIRECT_KEY = 'im_sso_redirect'

// 仅接受站内路径，拒绝 //host、javascript: 等开放跳转
export function sanitizeRedirectTarget(target: string | null | undefined): string | null {
  if (!target || !target.startsWith('/') || target.startsWith('//')) return null
  return target
}

export function stashSSORedirectTarget(target: string | null | undefined) {
  const safe = sanitizeRedirectTarget(target)
  if (safe) sessionStorage.setItem(SSO_REDIRECT_KEY, safe)
}

export function consumeSSORedirectTarget(): string | null {
  const raw = sessionStorage.getItem(SSO_REDIRECT_KEY)
  sessionStorage.removeItem(SSO_REDIRECT_KEY)
  return sanitizeRedirectTarget(raw)
}

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
  kingdee?: { enabled: boolean; base_url?: string; app_client_id?: string }
}

async function fetchSSOConfig(): Promise<SSOConfig> {
  const res = await fetch('/api/v1/auth/sso/config', { headers: { Accept: 'application/json' } })
  if (!res.ok) return {}
  return (await res.json()) as SSOConfig
}

/**
 * 金蝶苍穹统一门户登录入口：返回苍穹 authorize.do 免登链接。
 *
 * 与企微/飞书内置浏览器免登不同，苍穹的常规入口是用户在苍穹门户点
 * 菜单跳转；此函数用于登录页的「苍穹登录」按钮，把同样的授权链接
 * 暴露给未在苍穹内跳转的用户。redirect_uri 需与苍穹「第三方应用」
 * SSO 可信白名单登记的完整地址（含 app_client_id 与 response_code
 * 查询参数）一致，苍穹回跳时会追加 code。
 */
// 苍穹免登后的默认落地页（对话页）；须与租户设置里生成的白名单地址
// 逐字符一致（login_target 一并登记在白名单内）。
const KINGDEE_DEFAULT_LOGIN_TARGET = '/platform/creatChat'

export async function getKingdeeLoginURL(): Promise<string | null> {
  const cfg = await fetchSSOConfig()
  const k = cfg.kingdee
  if (!k?.enabled || !k.base_url || !k.app_client_id) return null
  const base = k.base_url.trim().replace(/\/+$/, '')
  const clientId = encodeURIComponent(k.app_client_id.trim())
  // 苍穹要求 redirect_uri 携带 app_client_id 与 response_code=code，
  // 且与白名单登记值逐字符一致（含 login_target）。
  const redirectUri = encodeURIComponent(
    `${window.location.origin}/api/v1/auth/sso/kingdee/callback?app_client_id=${k.app_client_id.trim()}&response_code=code&login_target=${encodeURIComponent(KINGDEE_DEFAULT_LOGIN_TARGET)}`,
  )
  return `${base}/auth/authorize.do?app_client_id=${clientId}&response_code=code&redirect_uri=${redirectUri}`
}

function buildAuthorizeURL(platform: IMSSOPlatform, cfg: SSOConfig, state: string): string | null {
  // Host 本身就是租户标识：授权回跳回到同域名 /login，后端按 Host 解析租户
  const redirectUri = encodeURIComponent(`${window.location.origin}/login`)
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
    window.location.href =
      `/api/v1/auth/sso/${platform}/callback?code=${encodeURIComponent(code)}`
    return { action: 'redirecting' }
  }

  // 后端失败回跳（#oidc_error 由 App.vue 统一展示，这里不重复处理）
  const cfg = await fetchSSOConfig()
  const state = randomState()
  const url = buildAuthorizeURL(platform, cfg, state)
  if (!url) return { action: 'none' }

  sessionStorage.setItem(SSO_STATE_KEY, state)
  stashSSORedirectTarget(params.get('redirect'))
  window.location.href = url
  return { action: 'redirecting' }
}
