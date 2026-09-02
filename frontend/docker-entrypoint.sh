#!/bin/sh

# Only emit whitelisted locale tags to avoid config.js injection from env values.
RUNTIME_DEFAULT_LOCALE=""
case "${DEFAULT_LOCALE:-}" in
  zh-CN|en-US|ru-RU|ko-KR) RUNTIME_DEFAULT_LOCALE="${DEFAULT_LOCALE}" ;;
esac

# 生成运行时配置文件，注入环境变量到前端
FILE_MB=${MAX_FILE_SIZE_MB:-50}
SKILL_MB=${MAX_SKILL_BUNDLE_SIZE_MB:-256}
if [ "$SKILL_MB" -lt "$FILE_MB" ] 2>/dev/null; then
  SKILL_MB=$FILE_MB
fi
if [ "$SKILL_MB" -gt 512 ] 2>/dev/null; then
  SKILL_MB=512
fi

cat > /usr/share/nginx/html/config.js << EOF
window.__RUNTIME_CONFIG__ = {
  MAX_FILE_SIZE_MB: ${FILE_MB},
  MAX_SKILL_BUNDLE_SIZE_MB: ${SKILL_MB},
  DEFAULT_LOCALE: "${RUNTIME_DEFAULT_LOCALE}"
};
EOF

# 处理 nginx 配置。
# 两个上限分开注入：全站保持知识库的 MAX_FILE_SIZE，只有技能 zip 上传的两条
# 集合路由放宽到 MAX_SKILL_BUNDLE_SIZE（不含 /install、PATCH 等子路径）。
# 合成一个全站上限会让每个上传端点都能收到技能包那么大的 body。
export MAX_FILE_SIZE=${FILE_MB}M
export MAX_SKILL_BUNDLE_SIZE=${SKILL_MB}M
export APP_HOST=${APP_HOST:-app}
export APP_PORT=${APP_PORT:-8080}
export APP_SCHEME=${APP_SCHEME:-http}

# 主站防嵌策略：默认 X-Frame-Options: SAMEORIGIN（防点击劫持）。
# 设置 FRAME_ALLOWED_ORIGINS（空格分隔的宿主站点 origin，如
# https://portal.example.com）后改为 CSP frame-ancestors 白名单，允许
# 这些站点以 iframe 集成主站（如金蝶苍穹门户菜单的 SSO 免登链接）。
# nginx 的 add_header 值为空字符串时不发送该头，两种模式共用同一份配置。
FRAME_ALLOWED_ORIGINS="${FRAME_ALLOWED_ORIGINS:-}"
if [ -n "$FRAME_ALLOWED_ORIGINS" ]; then
  export X_FRAME_OPTIONS=""
  export FRAME_CSP="frame-ancestors 'self' ${FRAME_ALLOWED_ORIGINS}"
else
  export X_FRAME_OPTIONS="SAMEORIGIN"
  export FRAME_CSP=""
fi

envsubst '${MAX_FILE_SIZE} ${MAX_SKILL_BUNDLE_SIZE} ${APP_HOST} ${APP_PORT} ${APP_SCHEME} ${X_FRAME_OPTIONS} ${FRAME_CSP}' \
  < /etc/nginx/templates/default.conf.template > /etc/nginx/conf.d/default.conf

# 启动 nginx
exec nginx -g 'daemon off;'
