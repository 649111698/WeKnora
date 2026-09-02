<template>
  <t-dialog :visible="visible" :footer="false" width="460px" dialog-class-name="batch-move-dialog"
    :close-on-overlay-click="false" destroy-on-close @close="handleClose">
    <template #header>
      <div class="batch-move-heading">
        <div class="batch-move-heading-row">
          <t-icon name="swap" size="16px" class="batch-move-heading-icon" aria-hidden="true" />
          <span class="batch-move-title">{{ $t('knowledgeBase.moveToKnowledgeBase') }}</span>
        </div>
        <p class="batch-move-subtitle">{{ $t('knowledgeBase.batchMoveSubtitle', { count }) }}</p>
      </div>
    </template>

    <div class="batch-move-body">
      <section class="batch-move-section">
        <h4 class="batch-move-section-title">{{ $t('knowledgeBase.moveTargetSection') }}</h4>
        <div v-if="loading" class="batch-move-loading">
          <t-loading size="small" />
        </div>
        <div v-else-if="targets.length === 0" class="batch-move-empty">
          {{ $t('knowledgeBase.moveNoTargets') }}
        </div>
        <div v-else class="batch-move-target-list">
          <button v-for="kb in targets" :key="kb.id" type="button" class="batch-move-target"
            :class="{ 'is-selected': selectedTargetId === kb.id }" @click="selectTarget(kb.id)">
            <t-icon name="root-list" size="16px" class="batch-move-target-icon" aria-hidden="true" />
            <span class="batch-move-target-name" :title="kb.name">{{ kb.name }}</span>
            <span v-if="kb.knowledge_count !== undefined" class="batch-move-target-count">{{ kb.knowledge_count }}</span>
            <t-icon v-if="selectedTargetId === kb.id" name="check" size="16px" class="batch-move-target-check" />
          </button>
        </div>
      </section>

      <section v-if="selectedTargetId" class="batch-move-section">
        <h4 class="batch-move-section-title">{{ $t('knowledgeBase.moveConfirmTitle') }}</h4>
        <div class="batch-move-target-info">
          <t-icon name="arrow-right" size="14px" />
          <span>{{ selectedTargetName }}</span>
        </div>
        <div class="batch-move-mode" :class="{ active: mode === 'reuse_vectors' }" role="button" tabindex="0"
          @click="mode = 'reuse_vectors'" @keydown.enter.prevent="mode = 'reuse_vectors'">
          <t-radio :checked="mode === 'reuse_vectors'" />
          <div class="batch-move-mode-text">
            <span class="batch-move-mode-label">{{ $t('knowledgeBase.moveModeReuseVectors') }}</span>
            <span class="batch-move-mode-desc">{{ $t('knowledgeBase.moveModeReuseVectorsDesc') }}</span>
          </div>
        </div>
        <div class="batch-move-mode" :class="{ active: mode === 'reparse' }" role="button" tabindex="0"
          @click="mode = 'reparse'" @keydown.enter.prevent="mode = 'reparse'">
          <t-radio :checked="mode === 'reparse'" />
          <div class="batch-move-mode-text">
            <span class="batch-move-mode-label">{{ $t('knowledgeBase.moveModeReparse') }}</span>
            <span class="batch-move-mode-desc">{{ $t('knowledgeBase.moveModeReparseDesc') }}</span>
          </div>
        </div>
      </section>
    </div>

    <div class="batch-move-footer">
      <span class="batch-move-footer-count">{{ $t('knowledgeBase.selectedCount', { count }) }}</span>
      <div class="batch-move-footer-right">
        <t-button variant="outline" size="small" :disabled="submitting" @click="handleClose">
          {{ $t('common.cancel') }}
        </t-button>
        <t-button theme="primary" size="small" :loading="submitting" :disabled="!selectedTargetId" @click="handleConfirm">
          {{ $t('knowledgeBase.moveConfirm') }}
        </t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';

export interface MoveTargetKB {
  id: string;
  name: string;
  knowledge_count?: number;
}

const props = defineProps<{
  visible: boolean;
  count: number;
  targets: MoveTargetKB[];
  loading?: boolean;
  submitting?: boolean;
}>();

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
  (e: 'confirm', payload: { targetKbId: string; targetKbName: string; mode: 'reuse_vectors' | 'reparse' }): void;
}>();

const selectedTargetId = ref('');
const selectedTargetName = ref('');
const mode = ref<'reuse_vectors' | 'reparse'>('reuse_vectors');

const selectTarget = (id: string) => {
  selectedTargetId.value = id;
  const kb = props.targets.find((t) => t.id === id);
  selectedTargetName.value = kb?.name || id;
  mode.value = 'reuse_vectors';
};

watch(
  () => props.visible,
  (val) => {
    if (val) {
      selectedTargetId.value = '';
      selectedTargetName.value = '';
      mode.value = 'reuse_vectors';
    }
  },
);

const handleClose = () => {
  emit('update:visible', false);
};

const handleConfirm = () => {
  if (!selectedTargetId.value) return;
  emit('confirm', {
    targetKbId: selectedTargetId.value,
    targetKbName: selectedTargetName.value,
    mode: mode.value,
  });
};
</script>

<style scoped lang="less">
.batch-move-heading-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.batch-move-heading-icon {
  color: var(--td-brand-color);
}

.batch-move-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.batch-move-subtitle {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.batch-move-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-height: 52vh;
  overflow-y: auto;
}

.batch-move-section-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.batch-move-loading,
.batch-move-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px 0;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.batch-move-target-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 220px;
  overflow-y: auto;
}

.batch-move-target {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  background: var(--td-bg-color-container);
  cursor: pointer;
  text-align: left;
  transition: all 0.15s ease;
  font-family: var(--app-font-family);

  &:hover {
    border-color: var(--td-brand-color);
  }

  &.is-selected {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }
}

.batch-move-target-icon {
  flex-shrink: 0;
  color: var(--td-text-color-secondary);
}

.batch-move-target.is-selected .batch-move-target-icon {
  color: var(--td-brand-color);
}

.batch-move-target-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: var(--td-text-color-primary);
}

.batch-move-target-count {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.batch-move-target-check {
  flex-shrink: 0;
  color: var(--td-brand-color);
}

.batch-move-target-info {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.batch-move-mode {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;

  & + & {
    margin-top: 8px;
  }

  &:hover {
    border-color: var(--td-brand-color);
  }

  &.active {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }
}

.batch-move-mode-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.batch-move-mode-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.batch-move-mode-desc {
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.batch-move-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 16px;
}

.batch-move-footer-count {
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.batch-move-footer-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
