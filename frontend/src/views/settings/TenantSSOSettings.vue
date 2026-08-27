<template>
  <div class="tenant-sso-settings">
    <!-- 专属登录域名：每个租户（企业）一个登录入口，域名是唯一的租户区分方式 -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.loginDomain.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.loginDomain.description') }}</p>
      <div class="sso-form">
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.loginDomain.label') }}</label>
          <t-input v-model="form.login_domain" :placeholder="t('tenantSSO.loginDomain.placeholder')"
            :disabled="saving" @enter="saveAll" />
        </div>
      </div>
    </section>

    <!-- 企微后台配置 URL 生成 -->
    <section v-if="form.login_domain" class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.guide.wecom.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.guide.wecom.description') }}</p>
      <div class="sso-form">
        <div v-for="row in wecomGuideRows" :key="row.label" class="sso-field">
          <label class="sso-field__label">{{ row.label }}</label>
          <div class="sso-copy-row">
            <code class="sso-code sso-code--block">{{ row.value }}</code>
            <t-button size="small" variant="outline" @click="copyText(row.value)">
              {{ t('common.copy') }}
            </t-button>
          </div>
        </div>
        <p class="sso-field__hint">{{ t('tenantSSO.guide.wecom.verifyNote') }}</p>
      </div>
    </section>

    <!-- 企业微信 SSO -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.wecom.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.wecom.description') }}</p>
      <div class="sso-form">
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.wecom.corpId') }}</label>
          <t-input v-model="form.wecom.corp_id" :placeholder="t('tenantSSO.wecom.corpIdPlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.wecom.corpSecret') }}</label>
          <t-input v-model="form.wecom.corp_secret" type="password"
            :placeholder="wecomSecretPlaceholder" :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.wecom.agentId') }}</label>
          <t-input v-model="form.wecom.agent_id" :placeholder="t('tenantSSO.wecom.agentIdPlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.wecom.domainVerifyText') }}</label>
          <t-input v-model="form.wecom.domain_verify_text"
            :placeholder="t('tenantSSO.wecom.domainVerifyPlaceholder')" :disabled="saving" />
          <p class="sso-field__hint">{{ t('tenantSSO.wecom.domainVerifyHint') }}</p>
        </div>
      </div>
    </section>

    <!-- 飞书 SSO -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.feishu.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.feishu.description') }}</p>
      <div class="sso-form">
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.feishu.appId') }}</label>
          <t-input v-model="form.feishu.app_id" :placeholder="t('tenantSSO.feishu.appIdPlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.feishu.appSecret') }}</label>
          <t-input v-model="form.feishu.app_secret" type="password"
            :placeholder="feishuSecretPlaceholder" :disabled="saving" />
        </div>
        <div v-if="form.login_domain" class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.guide.feishu.redirectUrl') }}</label>
          <div class="sso-copy-row">
            <code class="sso-code sso-code--block">{{ loginUrl }}</code>
            <t-button size="small" variant="outline" @click="copyText(loginUrl)">
              {{ t('common.copy') }}
            </t-button>
          </div>
        </div>
      </div>
    </section>

    <!-- 金蝶苍穹 SSO -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.kingdee.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.kingdee.description') }}</p>
      <div class="sso-form">
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.baseUrl') }}</label>
          <t-input v-model="form.kingdee.base_url" :placeholder="t('tenantSSO.kingdee.baseUrlPlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.appClientId') }}</label>
          <t-input v-model="form.kingdee.app_client_id" :placeholder="t('tenantSSO.kingdee.appClientIdPlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.appSecret') }}</label>
          <t-input v-model="form.kingdee.app_secret" type="password"
            :placeholder="kingdeeSecretPlaceholder" :disabled="saving" />
          <p class="sso-field__hint">{{ t('tenantSSO.kingdee.appSecretHint') }}</p>
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.proxyUsername') }}</label>
          <t-input v-model="form.kingdee.username" :placeholder="t('tenantSSO.kingdee.proxyUsernamePlaceholder')"
            :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.accountId') }}</label>
          <t-input v-model="form.kingdee.account_id" :placeholder="t('tenantSSO.kingdee.accountIdPlaceholder')"
            :disabled="saving" />
          <p class="sso-field__hint">{{ t('tenantSSO.kingdee.tokenModeHint') }}</p>
        </div>
        <div v-if="kingdeeCallbackUrl" class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.callbackUrl') }}</label>
          <div class="sso-copy-row">
            <code class="sso-code sso-code--block">{{ kingdeeCallbackUrl }}</code>
            <t-button size="small" variant="outline" @click="copyText(kingdeeCallbackUrl)">
              {{ t('common.copy') }}
            </t-button>
          </div>
          <p class="sso-field__hint">{{ t('tenantSSO.kingdee.callbackUrlHint') }}</p>
        </div>
        <div v-if="kingdeeMenuUrl" class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.kingdee.menuUrl') }}</label>
          <div class="sso-copy-row">
            <code class="sso-code sso-code--block">{{ kingdeeMenuUrl }}</code>
            <t-button size="small" variant="outline" @click="copyText(kingdeeMenuUrl)">
              {{ t('common.copy') }}
            </t-button>
          </div>
          <p class="sso-field__hint">{{ t('tenantSSO.kingdee.menuUrlHint') }}</p>
        </div>
      </div>
    </section>

    <!-- 水印（租户级） -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.watermark.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.watermark.description') }}</p>
      <div class="sso-form">
        <div class="sso-field sso-field--inline">
          <label class="sso-field__label">{{ t('tenantSSO.watermark.enabled') }}</label>
          <t-switch v-model="watermark.enabled" :disabled="saving" />
        </div>
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.watermark.text') }}</label>
          <t-input v-model="watermark.text" :placeholder="t('tenantSSO.watermark.textPlaceholder')"
            :disabled="saving || !watermark.enabled" />
          <p class="sso-field__hint">{{ t('tenantSSO.watermark.textHint') }}</p>
        </div>
      </div>
    </section>

    <div class="sso-actions">
      <t-button theme="primary" :loading="saving" @click="saveAll">{{ t('common.save') }}</t-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { get, put } from '@/utils/request'
import { copyToClipboard } from '@/utils/clipboard'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const SECRET_MASK = '***'

type SSOForm = {
  login_domain: string
  wecom: { corp_id: string; corp_secret: string; agent_id: string; domain_verify_text: string }
  feishu: { app_id: string; app_secret: string }
  kingdee: { base_url: string; app_client_id: string; app_secret: string; username: string; account_id: string }
}

const form = reactive<SSOForm>({
  login_domain: '',
  wecom: { corp_id: '', corp_secret: '', agent_id: '', domain_verify_text: '' },
  feishu: { app_id: '', app_secret: '' },
  kingdee: { base_url: '', app_client_id: '', app_secret: '', username: '', account_id: '' },
})
const watermark = reactive({ enabled: false, text: '' })

const wecomSecretConfigured = ref(false)
const feishuSecretConfigured = ref(false)
const kingdeeSecretConfigured = ref(false)
const saving = ref(false)

const wecomSecretPlaceholder = computed(() =>
  wecomSecretConfigured.value ? t('tenantSSO.keepSecretPlaceholder') : t('tenantSSO.wecom.corpSecretPlaceholder'))
const feishuSecretPlaceholder = computed(() =>
  feishuSecretConfigured.value ? t('tenantSSO.keepSecretPlaceholder') : t('tenantSSO.feishu.appSecretPlaceholder'))

const kingdeeSecretPlaceholder = computed(() =>
  kingdeeSecretConfigured.value ? t('tenantSSO.keepSecretPlaceholder') : t('tenantSSO.kingdee.appSecretPlaceholder'))

// 苍穹免登后的默认落地页（对话页）；作为 login_target 随 redirect_uri
// 透传，登录完成后前端跳转到该页而不是默认首页。
const KINGDEE_DEFAULT_LOGIN_TARGET = '/platform/creatChat'

// 苍穹「第三方应用 → 访问策略 → SSO 可信白名单」需登记的完整回调地址：
// 苍穹回跳时按该地址追加 code 参数，登记值必须与其逐字符一致
// （含 login_target 参数）。
const kingdeeCallbackUrl = computed(() => {
  if (!form.login_domain || !form.kingdee.app_client_id.trim()) return ''
  const host = form.login_domain.trim().replace(/^https?:\/\//, '')
  return `https://${host}/api/v1/auth/sso/kingdee/callback?app_client_id=${form.kingdee.app_client_id.trim()}&response_code=code&login_target=${encodeURIComponent(KINGDEE_DEFAULT_LOGIN_TARGET)}`
})

// 苍穹门户菜单/快速发起配置的「免登链接」：已登录苍穹的用户点击即带
// 授权码跳转本系统，无需二次登录。
const kingdeeMenuUrl = computed(() => {
  const base = form.kingdee.base_url.trim().replace(/\/+$/, '')
  if (!base || !kingdeeCallbackUrl.value) return ''
  const clientId = encodeURIComponent(form.kingdee.app_client_id.trim())
  const redirectUri = encodeURIComponent(kingdeeCallbackUrl.value)
  return `${base}/auth/authorize.do?app_client_id=${clientId}&response_code=code&redirect_uri=${redirectUri}`
})

const loginUrl = computed(() => {
  if (!form.login_domain) return ''
  return `https://${form.login_domain.trim().replace(/^https?:\/\//, '')}/login`
})

const loginHost = computed(() => {
  try {
    return new URL(loginUrl.value).host
  } catch {
    return form.login_domain
  }
})

// 企微后台需要的三个值：工作台应用主页、可信域名、可信 IP 段不填
const wecomGuideRows = computed(() => [
  { label: t('tenantSSO.guide.wecom.homeUrl'), value: loginUrl.value },
  { label: t('tenantSSO.guide.wecom.trustedDomain'), value: loginHost.value },
])

const copyText = async (text: string) => {
  const ok = await copyToClipboard(text)
  if (ok) MessagePlugin.success(t('common.copied'))
}

const loadConfig = async () => {
  try {
    const resp: any = await get('/api/v1/tenants/kv/sso-config')
    const cfg = resp?.data
    if (cfg) {
      form.login_domain = cfg.login_domain || ''
      if (cfg.wecom) {
        form.wecom.corp_id = cfg.wecom.corp_id || ''
        form.wecom.agent_id = cfg.wecom.agent_id || ''
        form.wecom.domain_verify_text = cfg.wecom.domain_verify_text || ''
        wecomSecretConfigured.value = !!cfg.wecom.corp_secret
      }
      if (cfg.feishu) {
        form.feishu.app_id = cfg.feishu.app_id || ''
        feishuSecretConfigured.value = !!cfg.feishu.app_secret
      }
      if (cfg.kingdee) {
        form.kingdee.base_url = cfg.kingdee.base_url || ''
        form.kingdee.app_client_id = cfg.kingdee.app_client_id || ''
        form.kingdee.username = cfg.kingdee.username || ''
        form.kingdee.account_id = cfg.kingdee.account_id || ''
        kingdeeSecretConfigured.value = !!cfg.kingdee.app_secret
      }
    }
  } catch {
    MessagePlugin.error(t('tenantSSO.loadFailed'))
  }
  try {
    const resp: any = await get('/api/v1/tenants/kv/watermark-config')
    if (resp?.data) {
      watermark.enabled = !!resp.data.enabled
      watermark.text = resp.data.text || ''
    }
  } catch {
    // 水印加载失败不打扰（保持默认关闭）
  }
}

const saveAll = async () => {
  const hasWecom = !!(form.wecom.corp_id.trim() || form.wecom.corp_secret)
  const hasFeishu = !!(form.feishu.app_id.trim() || form.feishu.app_secret)
  const hasKingdee = !!(form.kingdee.base_url.trim() || form.kingdee.app_client_id.trim() || form.kingdee.app_secret || form.kingdee.username.trim() || form.kingdee.account_id.trim())
  if ((hasWecom || hasFeishu || hasKingdee) && !form.login_domain.trim()) {
    MessagePlugin.warning(t('tenantSSO.loginDomain.required'))
    return
  }
  saving.value = true
  try {
    const payload = {
      login_domain: form.login_domain.trim(),
      wecom: {
        corp_id: form.wecom.corp_id.trim(),
        corp_secret: form.wecom.corp_secret,
        agent_id: form.wecom.agent_id.trim(),
        domain_verify_text: form.wecom.domain_verify_text.trim(),
      },
      feishu: {
        app_id: form.feishu.app_id.trim(),
        app_secret: form.feishu.app_secret,
      },
      kingdee: {
        base_url: form.kingdee.base_url.trim().replace(/\/+$/, ''),
        app_client_id: form.kingdee.app_client_id.trim(),
        app_secret: form.kingdee.app_secret,
        username: form.kingdee.username.trim(),
        account_id: form.kingdee.account_id.trim(),
      },
    }
    const resp: any = await put('/api/v1/tenants/kv/sso-config', payload)
    const cfg = resp?.data
    if (cfg) {
      wecomSecretConfigured.value = !!(cfg.wecom?.corp_secret)
      feishuSecretConfigured.value = !!(cfg.feishu?.app_secret)
      kingdeeSecretConfigured.value = !!(cfg.kingdee?.app_secret)
      form.wecom.corp_secret = ''
      form.feishu.app_secret = ''
      form.kingdee.app_secret = ''
    }
    await put('/api/v1/tenants/kv/watermark-config', {
      enabled: watermark.enabled,
      text: watermark.enabled ? watermark.text : '',
    })
    MessagePlugin.success(t('tenantSSO.saveSuccess'))
  } catch (e: any) {
    MessagePlugin.error(e?.response?.data?.error || t('tenantSSO.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(loadConfig)
</script>

<style scoped lang="less">
.tenant-sso-settings {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 640px;
}

.sso-card {
  padding: 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
}

.sso-card__title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.sso-card__desc {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.sso-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.sso-field__label {
  display: block;
  margin-bottom: 4px;
  font-size: 13px;
  color: var(--td-text-color-primary);
}

.sso-field--inline {
  display: flex;
  align-items: center;
  justify-content: space-between;

  .sso-field__label {
    margin-bottom: 0;
  }
}

.sso-field__hint {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-text-color-placeholder);
}

.sso-code {
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--td-bg-color-component);
  font-size: 12px;
  user-select: all;
}

.sso-copy-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sso-code--block {
  flex: 1;
  padding: 6px 8px;
  overflow-x: auto;
  white-space: nowrap;
}

.sso-actions {
  display: flex;
  justify-content: flex-end;
}
</style>
