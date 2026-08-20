<template>
  <div class="tenant-sso-settings">
    <!-- 专属登录域名：每个租户（企业）一个登录入口 -->
    <section class="sso-card">
      <h3 class="sso-card__title">{{ t('tenantSSO.loginDomain.title') }}</h3>
      <p class="sso-card__desc">{{ t('tenantSSO.loginDomain.description') }}</p>
      <div class="sso-form">
        <div class="sso-field">
          <label class="sso-field__label">{{ t('tenantSSO.loginDomain.label') }}</label>
          <t-input v-model="form.login_domain" :placeholder="t('tenantSSO.loginDomain.placeholder')"
            :disabled="saving" @enter="saveAll" />
        </div>
        <p v-if="loginUrl" class="sso-field__hint">
          {{ t('tenantSSO.loginDomain.entryHint') }}:
          <code class="sso-code">{{ loginUrl }}</code>
        </p>
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
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const SECRET_MASK = '***'

type SSOForm = {
  login_domain: string
  wecom: { corp_id: string; corp_secret: string; agent_id: string; domain_verify_text: string }
  feishu: { app_id: string; app_secret: string }
}

const form = reactive<SSOForm>({
  login_domain: '',
  wecom: { corp_id: '', corp_secret: '', agent_id: '', domain_verify_text: '' },
  feishu: { app_id: '', app_secret: '' },
})
const watermark = reactive({ enabled: false, text: '' })

const wecomSecretConfigured = ref(false)
const feishuSecretConfigured = ref(false)
const saving = ref(false)

const wecomSecretPlaceholder = computed(() =>
  wecomSecretConfigured.value ? t('tenantSSO.keepSecretPlaceholder') : t('tenantSSO.wecom.corpSecretPlaceholder'))
const feishuSecretPlaceholder = computed(() =>
  feishuSecretConfigured.value ? t('tenantSSO.keepSecretPlaceholder') : t('tenantSSO.feishu.appSecretPlaceholder'))

const loginUrl = computed(() => {
  if (!form.login_domain) return ''
  return `https://${form.login_domain}/login`
})

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
    }
    const resp: any = await put('/api/v1/tenants/kv/sso-config', payload)
    const cfg = resp?.data
    if (cfg) {
      wecomSecretConfigured.value = !!(cfg.wecom?.corp_secret)
      feishuSecretConfigured.value = !!(cfg.feishu?.app_secret)
      form.wecom.corp_secret = ''
      form.feishu.app_secret = ''
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

.sso-actions {
  display: flex;
  justify-content: flex-end;
}
</style>
