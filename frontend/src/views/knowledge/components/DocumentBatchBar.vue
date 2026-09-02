<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import FolderPickerMenu, { type FolderOption } from './FolderPickerMenu.vue';

defineProps<{
  count: number;
  deleteLoading?: boolean;
  reparseLoading?: boolean;
  tagLoading?: boolean;
  /** Batch download of the original files (ZIP) — hidden when download is not allowed. */
  canDownload?: boolean;
  downloadLoading?: boolean;
  /** Move the selection to another knowledge base — hidden without mutate permission. */
  canMoveKb?: boolean;
  // When true the bar stays visible even with 0 selections, so users can exit
  // batch mode from here without selecting anything first.
  visible?: boolean;
  /** Hidden when the knowledge base has no folder structure to file into. */
  showMoveToFolder?: boolean;
  folderOptions?: FolderOption[];
}>();

const emit = defineEmits<{
  (e: 'cancel'): void;
  (e: 'delete'): void;
  (e: 'reparse'): void;
  (e: 'batchTag'): void;
  (e: 'download'): void;
  (e: 'moveKb'): void;
  (e: 'moveToFolder', folderPath: string): void;
}>();

const { t } = useI18n();

const folderPickerVisible = ref(false);
</script>

<template>
  <transition name="batch-bar-fade">
    <div v-if="visible || count > 0" class="doc-batch-bar" role="region"
      :aria-label="t('knowledgeBase.selectedCount', { count })">
      <div class="batch-bar-inner">
        <div class="batch-bar-left">
          <span class="batch-bar-count">{{ t('knowledgeBase.selectedCount', { count }) }}</span>
          <t-button variant="text" theme="default" size="small" class="batch-bar-clear" @click="emit('cancel')">
            {{ t('knowledgeBase.clearSelection') }}
          </t-button>
        </div>
        <div class="batch-bar-actions">
          <t-popconfirm theme="warning" :content="t('knowledgeBase.confirmBatchReparseDocument', { count })"
            :confirm-btn="{ content: t('knowledgeBase.confirmBatchReparse'), theme: 'warning' }"
            :cancel-btn="{ content: t('common.cancel') }" placement="top" @confirm="emit('reparse')">
            <t-button theme="default" variant="outline" size="small"
              :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading" :loading="reparseLoading" @click.stop>
              <template #icon><t-icon name="refresh" size="14px" /></template>
              {{ t('knowledgeBase.rebuildDocument') }}
            </t-button>
          </t-popconfirm>

          <t-button theme="default" variant="outline" size="small"
            :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading" :loading="tagLoading"
            @click="emit('batchTag')">
            <template #icon><t-icon name="discount" size="14px" /></template>
            {{ t('knowledgeBase.batchTag') }}
          </t-button>

          <t-button v-if="canMoveKb" theme="default" variant="outline" size="small"
            :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading || downloadLoading"
            @click="emit('moveKb')">
            <template #icon><t-icon name="swap" size="14px" /></template>
            {{ t('knowledgeBase.moveToKnowledgeBase') }}
          </t-button>

          <t-button v-if="canDownload" theme="default" variant="outline" size="small"
            :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading" :loading="downloadLoading"
            @click="emit('download')">
            <template #icon><t-icon name="download" size="14px" /></template>
            {{ t('knowledgeBase.batchDownload') }}
          </t-button>

          <t-popup v-if="showMoveToFolder" v-model:visible="folderPickerVisible" trigger="click"
            placement="top" overlay-class-name="card-more" destroy-on-close>
            <t-button theme="default" variant="outline" size="small"
              :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading">
              <template #icon><t-icon name="folder" size="14px" /></template>
              {{ t('knowledgeBase.moveToFolder.action') }}
            </t-button>
            <template #content>
              <div class="card-menu">
                <FolderPickerMenu :options="folderOptions || []"
                  @confirm="(path: string) => { folderPickerVisible = false; emit('moveToFolder', path) }" />
              </div>
            </template>
          </t-popup>

          <t-popconfirm theme="warning" :content="t('knowledgeBase.confirmBatchDeleteDocument', { count })"
            :confirm-btn="{ content: t('knowledgeBase.confirmDelete'), theme: 'danger' }"
            :cancel-btn="{ content: t('common.cancel') }" placement="top" @confirm="emit('delete')">
            <t-button theme="danger" variant="outline" size="small"
              :disabled="count === 0 || deleteLoading || reparseLoading || tagLoading" :loading="deleteLoading" @click.stop>
              <template #icon><t-icon name="delete" size="14px" /></template>
              {{ t('knowledgeBase.batchDelete') }}
            </t-button>
          </t-popconfirm>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped lang="less">
.doc-batch-bar {
  position: relative;
  z-index: 5;
  width: 100%;
  // 6 个操作按钮单行约 750px；放不下时靠下面的 wrap 换行，不再溢出容器。
  max-width: min(760px, 100%);
  margin: 0 auto;
  padding: 0 4px;
  box-sizing: border-box;
}

.batch-bar-inner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
  padding: 8px 12px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.08);
}

.batch-bar-left {
  display: flex;
  align-items: center;
  gap: 4px;
  // 按内容参与换行计算：放不下时整个按钮组换到第二行，而不是把计数文字挤出容器。
  flex: 0 1 auto;
}

.batch-bar-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-secondary);
  white-space: nowrap;
}

.batch-bar-clear {
  flex-shrink: 0;
  padding: 0 6px !important;
  height: 28px !important;
  font-size: 12px;
  white-space: nowrap;
  color: var(--td-text-color-secondary) !important;

  &:hover {
    color: var(--td-brand-color) !important;
  }
}

.batch-bar-actions {
  flex-shrink: 0;
  // 按钮组超出条宽时必须在组内换行；不设上限它会按内容伸到 600px+，
  // 把末尾按钮顶出批量条。
  max-width: 100%;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;

  // 按钮文字不允许折行，避免窄屏下按钮被竖排挤变形。
  :deep(.t-button) {
    white-space: nowrap;
  }
}

.batch-bar-fade-enter-active,
.batch-bar-fade-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.batch-bar-fade-enter-from,
.batch-bar-fade-leave-to {
  opacity: 0;
  transform: translateY(6px);
}
</style>
