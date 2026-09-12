import type { CapacitorConfig } from '@capacitor/cli';

/**
 * Capacitor 移动端打包配置
 * 严格对齐避坑指南 ho_1fb00362abf9：
 * 1. 声明 androidScheme: 'https' 与 hostname: 'localhost'，使 WebView 请求携带 Origin: https://localhost，命中后端跨域白名单，解决 WS 403 拦截问题；
 * 2. 允许明文与混合通信，配合 Android network_security_config.xml 实现自签证书穿透。
 */
const config: CapacitorConfig = {
  appId: 'com.neo.golangim',
  appName: 'Golang IM Neo',
  webDir: 'dist',
  server: {
    androidScheme: 'https',
    hostname: 'localhost',
    cleartext: true,
  },
  android: {
    allowMixedContent: true,
  },
};

export default config;
