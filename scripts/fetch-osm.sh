#!/usr/bin/env bash
# 下载 Geofabrik 的中国 OSM 提取数据,供 Valhalla 自托管建瓦片。
# 用法: bash scripts/fetch-osm.sh
# 数据约 1.1GB;首次 docker compose up -d valhalla 会自动建瓦片,耗时较长。
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)/valhalla-data"
URL="https://download.geofabrik.de/asia/china-latest.osm.pbf"
FILE="$DIR/china-latest.osm.pbf"

mkdir -p "$DIR"
if [ -f "$FILE" ]; then
  echo "已存在 $FILE,跳过下载(如需更新请删除后重跑)"
  exit 0
fi

echo "下载 $URL"
curl -L --progress-bar -o "$FILE" "$URL"
echo "完成:$FILE"
echo "下一步:docker compose up -d valhalla(首次启动自动建瓦片,请耐心等待 /healthz 就绪)"
