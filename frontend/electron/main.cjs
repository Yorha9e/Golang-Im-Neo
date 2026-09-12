const { app, BrowserWindow, session } = require('electron');
const path = require('path');
const fs = require('fs');
const http = require('http');
const https = require('https');

// 允许自签证书与不安全 localhost
app.commandLine.appendSwitch('ignore-certificate-errors');
app.commandLine.appendSwitch('allow-insecure-localhost', 'true');

let mainWindow = null;
let localServer = null;
let localPort = 28080;

const REMOTE_API_HOST = '106.52.170.56';
const REMOTE_API_PORT = 8080;
const httpsAgent = new https.Agent({ rejectUnauthorized: false });

const MIME_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.webp': 'image/webp',
  '.wav': 'audio/wav',
  '.mp3': 'audio/mpeg',
  '.mp4': 'video/mp4',
  '.ico': 'image/x-icon',
};

// 启动极轻量本地静态托管与云端 API 反代服务 (绑定固定端口以永久固化 localStorage 登录态)
function startLocalServer() {
  return new Promise((resolve) => {
    const distDir = path.join(__dirname, '../dist');
    const FIXED_PORT = 28080;

    localServer = http.createServer((req, res) => {
      // 1. API 与多媒体反向代理到云端真机
      if (req.url.startsWith('/api/')) {
        const proxyReq = https.request(
          {
            hostname: REMOTE_API_HOST,
            port: REMOTE_API_PORT,
            path: req.url,
            method: req.method,
            headers: {
              ...req.headers,
              host: `${REMOTE_API_HOST}:${REMOTE_API_PORT}`,
            },
            agent: httpsAgent,
          },
          (proxyRes) => {
            res.writeHead(proxyRes.statusCode, proxyRes.headers);
            proxyRes.pipe(res);
          }
        );

        proxyReq.on('error', (err) => {
          console.error('[Proxy Error]:', err.message);
          res.writeHead(502, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ code: 50001, msg: '网关连接失败' }));
        });

        req.pipe(proxyReq);
        return;
      }

      // 2. 静态资源托管
      let reqPath = req.url.split('?')[0];
      if (reqPath === '/' || reqPath === '') {
        reqPath = '/index.html';
      }

      let filePath = path.join(distDir, reqPath);
      if (!fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
        filePath = path.join(distDir, 'index.html');
      }

      const ext = path.extname(filePath).toLowerCase();
      const contentType = MIME_TYPES[ext] || 'application/octet-stream';

      fs.readFile(filePath, (err, content) => {
        if (err) {
          res.writeHead(404);
          res.end('Not Found');
          return;
        }
        res.writeHead(200, { 'Content-Type': contentType });
        res.end(content);
      });
    });

    localServer.on('error', (e) => {
      if (e.code === 'EADDRINUSE') {
        console.warn(`⚠️ 端口 ${FIXED_PORT} 被占用，随机分配空闲端口`);
        localServer.listen(0, '127.0.0.1', () => {
          localPort = localServer.address().port;
          resolve(localPort);
        });
      }
    });

    localServer.listen(FIXED_PORT, '127.0.0.1', () => {
      localPort = FIXED_PORT;
      console.log(`🚀 [Embedded Server] 本地固定网关已就绪 (登录态永久固化): http://127.0.0.1:${FIXED_PORT}`);
      resolve(FIXED_PORT);
    });
  });
}

async function createWindow() {
  const port = await startLocalServer();

  mainWindow = new BrowserWindow({
    width: 1240,
    height: 840,
    minWidth: 980,
    minHeight: 680,
    title: 'Golang IM Neo - 细腻暖沙与暖阳',
    backgroundColor: '#FAF8F5',
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      nodeIntegration: false,
      contextIsolation: true,
      webSecurity: true,
      allowRunningInsecureContent: false,
      partition: 'persist:golang-im-neo',
    },
  });

  // 1. 自动放行媒体权限 (麦克风、摄像头、通知)
  session.defaultSession.setPermissionRequestHandler((webContents, permission, callback) => {
    const allowed = ['media', 'mediaKeySystem', 'notifications', 'microphone', 'camera'];
    if (allowed.includes(permission)) {
      console.log(`🎙️ [Media Auth] 自动授予原生媒体权限: ${permission}`);
      callback(true);
    } else {
      callback(false);
    }
  });

  session.defaultSession.setPermissionCheckHandler((webContents, permission) => {
    const allowed = ['media', 'mediaKeySystem', 'notifications', 'microphone', 'camera'];
    return allowed.includes(permission);
  });

  // 2. 加载页面
  const isDev = process.env.NODE_ENV === 'development' || process.argv.includes('--dev');
  if (isDev) {
    mainWindow.loadURL('http://localhost:5173');
    mainWindow.webContents.openDevTools({ mode: 'detach' });
  } else {
    mainWindow.loadURL(`http://127.0.0.1:${port}`);
  }

  // 注册 F12 快捷键
  mainWindow.webContents.on('before-input-event', (event, input) => {
    if (input.key === 'F12' && input.type === 'keyDown') {
      mainWindow.webContents.toggleDevTools();
      event.preventDefault();
    }
  });

  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

// 证书错误拦截放行
app.on('certificate-error', (event, webContents, url, error, certificate, callback) => {
  const parsedUrl = new URL(url);
  if (
    parsedUrl.hostname === REMOTE_API_HOST ||
    parsedUrl.hostname === 'localhost' ||
    parsedUrl.hostname === '127.0.0.1'
  ) {
    event.preventDefault();
    callback(true);
  } else {
    callback(false);
  }
});

app.whenReady().then(() => {
  createWindow();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on('window-all-closed', () => {
  if (localServer) {
    localServer.close();
  }
  if (process.platform !== 'darwin') {
    app.quit();
  }
});
