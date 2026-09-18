# Route66 前端原型 → Figma 使用说明

## 包内容

```
figma-prototype/
├── screenshots/          # 7 张关键状态整页截图（1440×900，PNG）
│   ├── 01-用户端-空态.png           # 初始页面：搜索 + 空列表 + 地图
│   ├── 02-用户端-规划结果.png        # 三点规划完成，地图画出路网
│   ├── 02b-用户端-结果卡片.png       # 左栏滚到底：规划结果卡（顺序 + 总距离）
│   ├── 03-用户端-设置弹层.png        # JS key / 安全密钥设置弹层
│   ├── 04-用户端-登录弹层.png        # 登录/注册弹层
│   ├── 05-管理台-登录.png            # 管理台登录页（7801）
│   └── 06-管理台-控制面板.png        # 管理台主界面：key 管理 + 用户列表
├── web/                  # 用户端构建产物（React + Vite，纯静态）
└── admin/                # 管理台单页（原生 HTML）
```

## 两种导入方式

### 方式 A：截图当画板（最快）

把 `screenshots/` 里的 PNG 直接拖进 Figma 画布，每张就是一块画板。
适合做点击跳转的原型流（Figma 里给热区链接到下一张画板即可）。

### 方式 B：html.to.design 插件导入可编辑图层

1. Figma 安装社区插件 **html.to.design**
2. 本地把静态页面跑起来（产物是纯静态，任选一个端口）：
   ```bash
   cd figma-prototype/web && python3 -m http.server 8090
   # 管理台另开:cd figma-prototype/admin && python3 -m http.server 8091
   ```
3. 插件里 Import from URL → `http://localhost:8090`（管理台 `http://localhost:8091`）
4. 导入后得到**可编辑的图层树**（文字、色块、圆角都是真的），适合做组件化改稿

> 注意：地图区域是高德 JS SDK 动态渲染的 canvas，导入后是一整块位图 ——
> 建议在 Figma 里用截图垫底，重新画图钉/路线这些覆盖物组件。

## 设计变量（tokens）

| 变量 | 值 | 用途 |
|---|---|---|
| `--ink` | `#1D1D1F` | 唯一交互色（按钮/文字/描边） |
| `--driving` | `#0066CC` | 驾车路线（蓝） |
| `--walking` | `#34A853` | 步行路线（绿） |
| `--transit` | `#E08600` | 公交路线（橙） |
| 背景/分割线 | `#F5F5F7` / `#E5E5EA` | 卡片与 hairline |

完整定义见仓库 `frontend/src/styles/tokens.css`。

## 各状态怎么复现（需要起完整后端时）

```bash
# 仓库根目录:Redis + PG 容器 + 后端
.redis-src/redis-7.2.5/src/redis-server --port 6379 --bind 127.0.0.1 ::1 --save '' --appendonly no &
docker compose up -d
go run .          # 7800 用户端 + 7801 管理台
```
