#!/usr/bin/env bash
# Route66 一键部署：前端构建 → 交叉编译 → 上传 → 重启
# 用法：./scripts/deploy.sh（SSH 需配置 Host blog）
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> 1/4 构建前端（产物输出到 web/）"
npm --prefix frontend run build

echo "==> 2/4 交叉编译 Linux 二进制"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o route66-linux-amd64 .

echo "==> 3/4 上传二进制与前端产物"
scp route66-linux-amd64 blog:/opt/route66/route66.new
scp -r web/ blog:/opt/route66/

echo "==> 4/4 原子替换并重启"
ssh blog 'mv /opt/route66/route66.new /opt/route66/route66 && systemctl restart route66 && sleep 2 && systemctl is-active route66 && curl -s http://127.0.0.1:7800/healthz'

echo "==> 完成：https://route66.bg5dux.top"
