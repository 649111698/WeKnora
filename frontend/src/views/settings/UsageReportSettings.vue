<template>
  <div class="usage-report-settings">
    <div class="settings-section-title">{{ t('usageReport.title') }}</div>
    <p class="settings-section-desc">{{ t('usageReport.desc') }}</p>

    <div v-if="loading" class="loading-wrap">{{ t('common.loading') }}</div>

    <template v-else>
      <div class="form-row">
        <div class="form-label">
          <div class="form-label-text">{{ t('usageReport.enable') }}</div>
          <div class="form-label-hint">{{ t('usageReport.enableHint') }}</div>
        </div>
        <t-switch v-model="cfg.enabled" />
      </div>

      <div class="form-row">
        <div class="form-label">
          <div class="form-label-text">{{ t('usageReport.pushToWeCom') }}</div>
          <div class="form-label-hint">{{ t('usageReport.pushToWeComHint') }}</div>
        </div>
        <t-switch v-model="cfg.push_to_wecom" :disabled="!cfg.enabled" />
      </div>

      <div class="form-row form-row-column">
        <div class="form-label">
          <div class="form-label-text">{{ t('usageReport.recipients') }}</div>
          <div class="form-label-hint">{{ t('usageReport.recipientsHint') }}</div>
        </div>
        <t-select v-model="cfg.notify_user_ids" multiple :placeholder="t('usageReport.recipientsPlaceholder')"
          :disabled="!cfg.enabled || !cfg.push_to_wecom" clearable :loading="membersLoading">
          <t-option v-for="m in members" :key="m.user_id" :value="m.user_id"
            :label="`${m.username || m.email}${isWeComMember(m) ? '（企业微信）' : ''}`" />
        </t-select>
      </div>

      <div class="rule-hint">{{ t('usageReport.rule') }}</div>

      <div class="actions">
        <t-button theme="primary" :loading="saving" :disabled="!cfg.enabled" @click="save">
          {{ t('common.save') }}
        </t-button>
        <t-button variant="outline" :loading="testing" :disabled="!cfg.enabled" @click="runTest">
          {{ t('usageReport.testNow') }}
        </t-button>
      </div>

      <div v-if="testResult" class="test-result">
        <div class="test-result-title">{{ t('usageReport.testResultTitle', { date: testResult.date }) }}</div>
        <div class="test-result-stats">
          <span>{{ t('usageReport.cardTotal', { n: testResult.total_users }) }}</span>
          <span>{{ t('usageReport.cardQualified', { n: testResult.qualified }) }}</span>
          <span>{{ t('usageReport.cardUnqualified', { n: testResult.unqualified }) }}</span>
          <span>{{ t('usageReport.cardMessages', { n: testResult.total_messages }) }}</span>
        </div>
        <pre class="test-result-preview">{{ testResult.content }}</pre>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import {
  getUsageReportConfig,
  updateUsageReportConfig,
  sendUsageReportTest,
  type UsageReportConfig,
  type UsageReportTestResult,
} from '@/api/tenant'
import { fetchAllTenantMembers, type TenantMember } from '@/api/tenant/members'

const { t } = useI18n()
const authStore = useAuthStore()

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const membersLoading = ref(false)
const members = ref<TenantMember[]>([])
const cfg = ref<UsageReportConfig>({ enabled: false, push_to_wecom: false, notify_user_ids: [] })
const testResult = ref<UsageReportTestResult | null>(null)

// 企微开号成员的合成邮箱（后端可推送应用消息的对象）。
const isWeComMember = (m: TenantMember) =>
  m.email?.startsWith('wecom_') && m.email?.endsWith('@wecom.sso.weknora.local')

const load = async () => {
  loading.value = true
  try {
    cfg.value = await getUsageReportConfig()
  } catch (e: any) {
    MessagePlugin.error(e?.response?.data?.error || t('usageReport.loadFailed'))
  } finally {
    loading.value = false
  }
}

const loadMembers = async () => {
  const tenantId = authStore.currentTenantId
  if (!tenantId) return
  membersLoading.value = true
  try {
    members.value = await fetchAllTenantMembers(Number(tenantId))
  } catch {
    // 成员列表加载失败不阻塞配置，选择器保持可搜索已加载部分。
  } finally {
    membersLoading.value = false
  }
}

const save = async () => {
  saving.value = true
  try {
    cfg.value = await updateUsageReportConfig({
      enabled: cfg.value.enabled,
      push_to_wecom: cfg.value.push_to_wecom,
      notify_user_ids: cfg.value.notify_user_ids ?? [],
    })
    MessagePlugin.success(t('common.saved'))
  } catch (e: any) {
    MessagePlugin.error(e?.response?.data?.error || t('usageReport.saveFailed'))
  } finally {
    saving.value = false
  }
}

const runTest = async () => {
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await sendUsageReportTest()
    MessagePlugin.success(t('usageReport.testSent'))
  } catch (e: any) {
    MessagePlugin.error(e?.response?.data?.error || t('usageReport.testFailed'))
  } finally {
    testing.value = false
  }
}

onMounted(() => {
  void load()
  void loadMembers()
})
</script>

<style scoped lang="less">
.usage-report-settings {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 12px 0;

  &.form-row-column {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }
}

.form-label-text {
  font-size: 14px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.form-label-hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.rule-hint {
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.actions {
  display: flex;
  gap: 12px;
}

.test-result {
  margin-top: 8px;

  .test-result-title {
    font-size: 13px;
    font-weight: 600;
    margin-bottom: 8px;
  }

  .test-result-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin-bottom: 8px;
  }

  .test-result-preview {
    max-height: 320px;
    overflow: auto;
    padding: 12px;
    border: 1px solid var(--td-component-stroke);
    border-radius: 8px;
    background: var(--td-bg-color-secondarycontainer);
    font-size: 12px;
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
  }
}
</style>
