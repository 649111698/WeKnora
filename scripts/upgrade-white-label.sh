#!/usr/bin/env bash
# 白标分支升级脚本（custom/white-label）
# 用法:
#   ./scripts/upgrade-white-label.sh              # 合并上游最新 main
#   ./scripts/upgrade-white-label.sh v0.7.3       # 合并指定 tag（推荐，跟稳定版）
#
# 流程: 拉取上游 → 合并到白标分支（有冲突会停下等你解决）→ 重建前端镜像并重启。
# 后端等其余服务继续用官方镜像，docker compose pull 升级即可，与本脚本无关。
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

BRANCH="custom/white-label"
UPSTREAM_REMOTE="upstream"
UPSTREAM_URL="https://gh-proxy.com/https://github.com/Tencent/WeKnora.git"

current_branch=$(git branch --show-current)
if [ "$current_branch" != "$BRANCH" ]; then
  echo ">> 当前分支是 $current_branch，切换到 $BRANCH"
  git checkout "$BRANCH"
fi

if ! git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1; then
  echo ">> 添加上游 remote: $UPSTREAM_URL"
  git remote add "$UPSTREAM_REMOTE" "$UPSTREAM_URL"
fi

echo ">> 拉取上游..."
git fetch "$UPSTREAM_REMOTE" --tags

TARGET="${1:-${UPSTREAM_REMOTE}/main}"
echo ">> 合并 $TARGET ..."
if ! git merge "$TARGET"; then
  echo
  echo "!! 出现冲突：请解决后 git add + git commit，然后重新运行本脚本继续构建。"
  echo "   白标改动集中在 frontend/src/assets/theme/white-label.css、"
  echo "   frontend/src/main.ts（一行 import）和 Login.vue（logo 三行），冲突通常就这三处。"
  exit 1
fi

echo ">> 构建前端静态资源..."
./scripts/build_frontend_dist.sh

echo ">> 重建 frontend 镜像并重启..."
docker compose build frontend
docker compose up -d frontend

echo
echo "✅ 升级完成。建议浏览器强刷（Cmd+Shift+R）确认外链仍处于隐藏状态。"
