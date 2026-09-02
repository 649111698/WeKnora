<template>
  <div class="sso-processing">
    <t-loading size="medium" />
    <p class="sso-processing-text">{{ $t('auth.ssoProcessing') }}</p>
  </div>
</template>

<script setup lang="ts">
// SSO/OIDC 回调落地的中转页：App.vue 正在用一次性 code 换取登录会话。
// 此页刻意不发起任何 API 请求、不依赖登录态——路由守卫把未登录的回调
// 落地导航先停在这里，避免业务页在会话建立前挂载（会触发 401→刷新→
// 登出的竞态）。会话建立后 App.vue 会 router.replace 到目标页。
</script>

<style scoped>
.sso-processing {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  height: 100vh;
  background: var(--td-bg-color-page, #f5f6f7);
}

.sso-processing-text {
  margin: 0;
  font-size: 14px;
  color: var(--td-text-color-secondary);
}
</style>
