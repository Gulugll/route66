# RN App（app/）构建与运行指南

React Native 跨端 App，覆盖 iOS / macOS / Android / Windows 四端。
后端契约、设计 token 与 Web 前端（`frontend/`）完全一致；差异点见文末。

## 工程概览

| 项 | 值 |
|---|---|
| 框架 | React Native 0.88（bare / RN CLI，非 Expo）+ React 19.3 + TypeScript |
| 地图 | WebView 内嵌 Leaflet（`src/map/`），瓦片源 `tile.openstreetmap.de`（大陆实测可达） |
| 状态 | hooks（`src/hooks/`），api client（`src/api/`）与后端 wire contract 逐字段对齐 |
| 设计变量 | `src/theme/tokens.ts`（与 `frontend/src/styles/tokens.css` 同源） |
| 后端地址 | `src/config.ts` 的 `API_BASE_URL`，默认 `http://localhost:7800` |

一期范围 = 规划核心（搜索 / 加点 / 列表 / 混合出行 / TSP 规划 / 画线 / 降级警告）。
登录、社区模块暂未移植（Web 版可用）。

## JS 层开发与验证（无需任何原生环境）

```bash
cd app
npm install

npx tsc --noEmit                 # 类型检查，零错误为准
npx jest                         # 单测（api client / 桥协议 / 点逻辑 / useSearch / usePlan）
npx react-native bundle --platform ios --dev false \
  --entry-file index.js --bundle-output /tmp/rn-ios.bundle      # Metro 打包验证
npx react-native bundle --platform android --dev false \
  --entry-file index.js --bundle-output /tmp/rn-android.bundle
```

Metro 开发服务器（改代码实时生效，配合模拟器/真机）：

```bash
cd app && npm start              # 默认 8081
```

## 各端原生构建

### iOS（需 macOS + Xcode + CocoaPods）

```bash
# 前置：App Store 装 Xcode（预留 30~40GB 磁盘：本体 + 模拟器运行时 + DerivedData）
xcode-select --install
brew install cocoapods
xcodebuild -version              # 确认可用

cd app/ios && pod install && cd ..

# 模拟器运行
npm run ios

# 真机：Xcode 打开 app/ios/RoutePlanner.xcworkspace，
# 选 Team 签名后 Run；真机访问 localhost:7800 需同一局域网并把
# src/config.ts 的 API_BASE_URL 改成 Mac 的局域网 IP
```

### macOS（需 macOS + Xcode + CocoaPods）

```bash
cd app/ios && pod install && cd ..
# Xcode 打开 app/ios/RoutePlanner.xcworkspace，目标选 macOS scheme 直接 Run
```

### Android（已验证：纯命令行可出 APK，无需 Android Studio / Xcode）

本机已就位的环境（2026-11-03 搭建，全程无 Studio）：

| 组件 | 位置 / 版本 |
|---|---|
| JDK | Homebrew openjdk@21（`/usr/libexec/java_home`） |
| SDK | `~/Library/Android/sdk`（platform-tools、platforms/android-36、build-tools 36.0.0 与 37.0.0） |
| NDK | `~/Library/Android/sdk/ndk/28.2.13676358`（新架构 codegen C++ 编译必需） |
| CMake | `~/Library/Android/sdk/cmake/3.22.1` |
| 许可证 | `licenses/` 手工写入标准哈希（sdkmanager 在线接受因网络不可达未落盘） |

出真机 APK：

```bash
export JAVA_HOME=$(/usr/libexec/java_home)
export ANDROID_HOME=$HOME/Library/Android/sdk
cd app/android
./gradlew assembleRelease -PreactNativeArchitectures=arm64-v8a
# 产物：app/build/outputs/apk/release/app-release.apk（约 18MB，debug keystore 签名可直接安装）
```

真机安装与联调：

1. 把 APK 传到手机（微信文件传输 / adb install / 网盘均可）安装
2. 手机与 Mac 连同一 Wi-Fi；`src/config.ts` 的 `API_BASE_URL` 已写死本机 en0 地址
   `http://10.211.145.178:7800`（IP 变了改这行重打；应用内设置页后置）
3. Mac 上起后端（`PORT=7800`），macOS 防火墙首次弹窗需允许入站

SDK 安装备忘（dl.google.com 大陆不可达，sdkmanager 在线装失败，走腾讯镜像直链）：

- 镜像：`https://mirrors.cloud.tencent.com/AndroidSDK/`（含 repository2-4.xml 清单，可查确切文件名）
- 已装包：`platform-36_r02.zip`、`build-tools_r37_macosx.zip`、`build-tools_r36_macosx.zip`、
  `platform-tools_r37.0.1-darwin.zip`、`android-ndk-r28c-darwin.zip`、`cmake-3.22.1-darwin.zip`
- 解压即装（zip 内 `source.properties` 供 AGP 识别版本，无需 package.xml）；
  build-tools zip 顶层目录是内部代号（如 `android-16`/`android-37.0`），须重命名为版本号目录
- 模板默认 compileSdk 37 因镜像缺 android-37 平台包已降至 36（react-android 的 target 即 36，无 37 API 依赖）

### 桌面端（Electron 壳，已验证：无需 Xcode）

macOS/Windows 桌面端不走 RN（RN 的 mac 端必须 Xcode），用 Electron 包 web 版，代码零改动：

```bash
cd desktop
npm install                        # electron + @electron/packager（二进制走 npmmirror）
npm start                          # 开发：直接开窗口连 http://localhost:7800
npm run pack                       # 打包 dist/RoutePlanner.app（arm64）
```

- `main.js`：BrowserWindow 加载 `http://localhost:7800`，后端未启动时显示兜底提示页
- electron-packager 被安全中心拦时的手工打包三步（已验证）：
  `xattr -cr` 模板 → `ditto` 复制 `node_modules/electron/dist/Electron.app` 为
  `dist/RoutePlanner.app` → 资源塞 `Contents/Resources/app/`（main.js + package.json）→
  PlistBuddy 改 CFBundleName/Identifier
- 后端必须先起（端口 7800）；Windows 侧同一壳 `--platform=win32` 出 `.exe`（需 Windows 机器跑 packager）

### Windows（需 Windows 机器）

```bash
# 前置：Visual Studio 2022（含"使用 C++ 的桌面开发"工作负载）+ Node
cd app
npx react-native init 已完成（工程已就位），Windows 侧：
npx react-native run-windows
```

RN 0.88 的 react-native-windows 支持以官方仓库为准；本工程 JS 层四端共用，
Windows 侧仅首次需要 `yarn add react-native-windows` 相关模板对齐（构建机上操作）。

## 本仓库验证边界

- 已验证：tsc、jest、Metro bundle（ios/android）全绿；**Android release APK 已成功产出**；**桌面端 Electron 壳 .app 已产出并启动验证**
- 未验证：iOS / macOS-RN / Windows 原生构建（需 Xcode / VS / Windows 机器，本机未装）；RN 真机渲染待手机安装后确认
- RN 真机验证通过前，`frontend/`（Web 版）保留不删——它是唯一可用界面

## 与 Web 前端的差异

| 项 | Web（frontend/） | RN（app/） |
|---|---|---|
| 地图 | Leaflet 直挂 DOM | WebView 内嵌 Leaflet + postMessage 桥（`src/map/bridge.ts`） |
| 拖拽排序 | HTML5 DnD | 上移/下移按钮（RN 无原生 DnD，拖拽排序二期再议） |
| 设计变量 | tokens.css + theme.js | tokens.ts 单文件 |
| 构建产物 | web/（Go 托管） | 各端原生包 |
