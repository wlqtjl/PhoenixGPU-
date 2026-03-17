#!/usr/bin/env bash
# hack/init-helm-repo.sh
# 初始化 GitHub Pages 作为 Helm Chart 仓库（首次运行一次即可）
# 用法: bash hack/init-helm-repo.sh
set -euo pipefail

REPO="wlqtjl/PhoenixGPU-"
PAGES_URL="https://wlqtjl.github.io/PhoenixGPU-/charts"

echo "→ 初始化 Helm Chart 仓库（gh-pages 分支）"

# 创建 gh-pages 分支（如果不存在）
if git ls-remote --heads origin gh-pages | grep -q gh-pages; then
  echo "  gh-pages 分支已存在，跳过创建"
else
  git checkout --orphan gh-pages
  git rm -rf . --quiet
  mkdir -p charts
  cat > README.md <<EOF
# PhoenixGPU Helm Chart Repository

\`\`\`bash
helm repo add phoenixgpu ${PAGES_URL}
helm repo update
helm install phoenixgpu phoenixgpu/phoenixgpu \\
  --namespace phoenixgpu-system \\
  --create-namespace
\`\`\`
EOF
  # 空 chart index
  cat > charts/index.yaml <<EOF
apiVersion: v1
entries: {}
generated: "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
EOF
  git add .
  git commit -m "init: Helm chart repository"
  git push origin gh-pages
  git checkout main
  echo "  ✅ gh-pages 分支已创建并推送"
fi

echo ""
echo "  Helm 仓库地址: ${PAGES_URL}"
echo ""
echo "  验证:"
echo "    helm repo add phoenixgpu ${PAGES_URL}"
echo "    helm repo update"
echo "    helm search repo phoenixgpu"
