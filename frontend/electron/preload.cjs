const { contextBridge, ipcRenderer } = require('electron');

// 向渲染进程安全注入原生客户端能力与标识
contextBridge.exposeInMainWorld('electronAPI', {
  isElectron: true,
  platform: process.platform,
  version: '1.0.0',
});
