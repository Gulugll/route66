const { app, BrowserWindow } = require('electron')

// ponytail: 单一职责——把 7800 的 web 界面套个原生窗口；自动拉起后端、托盘、自定义图标等以后有需求再说
const BACKEND_URL = 'http://localhost:7800'

function createWindow() {
  const win = new BrowserWindow({
    width: 1360,
    height: 900,
    title: 'Route Planner',
    // 后端是本机 http 明文，Electron 渲染进程无跨域需求，维持默认即可
  })

  win.loadURL(BACKEND_URL).catch(() => {
    win.loadURL(
      'data:text/html;charset=utf-8,' +
        encodeURIComponent(
          '<body style="font-family:-apple-system,sans-serif;background:#1D1D1F;color:#f5f5f7;display:flex;align-items:center;justify-content:center;height:90vh;text-align:center"><div><h2>后端未启动</h2><p>请先在终端运行 Route Planner 后端（端口 7800），然后重启本应用。</p></div></body>'
        )
    )
  })
}

app.whenReady().then(createWindow)

app.on('window-all-closed', () => app.quit())
