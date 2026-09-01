import { defineStore } from 'pinia'
import { ref } from 'vue'
import { get } from '@/utils/request'
import { getAuthConfig } from '@/api/auth'

/** 租户白标品牌配置（tenants.branding_config）。空串 = 未配置，回退默认文案/Logo */
export interface BrandingData {
  welcome_title?: string
  login_title?: string
  login_subtitle?: string
  logo_url?: string
  sidebar_title?: string
}

/**
 * 品牌外观（白标）全局状态。
 *
 * 数据源与水印一致：登录前经公开的 /auth/config 搭车（后端按请求 Host
 * 匹配租户登录域名），登录后走 /tenants/kv/branding-config 按当前空间
 * 拉取。App.vue 在登录态变化时统一 load，应用点（登录页 / 新对话欢迎语 /
 * 侧边栏 Logo）只读 store，未配置字段一律回退默认。
 */
export const useBrandingStore = defineStore('branding', () => {
  const welcomeTitle = ref('')
  const loginTitle = ref('')
  const loginSubtitle = ref('')
  const logoUrl = ref('')
  const sidebarTitle = ref('')
  const loaded = ref(false)

  function apply(data?: BrandingData | null) {
    welcomeTitle.value = (data?.welcome_title || '').trim()
    loginTitle.value = (data?.login_title || '').trim()
    loginSubtitle.value = (data?.login_subtitle || '').trim()
    logoUrl.value = (data?.logo_url || '').trim()
    sidebarTitle.value = (data?.sidebar_title || '').trim()
    loaded.value = true
  }

  async function load(authed: boolean) {
    try {
      if (authed) {
        const resp = await get('/api/v1/tenants/kv/branding-config')
        apply(resp?.data)
        return
      }
      const cfg = await getAuthConfig()
      apply(cfg?.branding)
    } catch {
      apply(null)
    }
  }

  return { welcomeTitle, loginTitle, loginSubtitle, logoUrl, sidebarTitle, loaded, apply, load }
})
