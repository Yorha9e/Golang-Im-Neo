/**
 * 多媒体 URL 格式化工具 (解决移动端 ERR_FILE_NOT_FOUND 与自签 IP 路径解析问题)
 * 参考自交接避雷指南 ho_1fb00362abf9
 */

const SERVER_HOST = '106.52.170.56:8080';
const SERVER_BASE = `https://${SERVER_HOST}`;

/**
 * 判断当前是否运行在客户端壳体内 (Capacitor / Electron / file://)
 * 壳体内 location.origin 是虚拟域 (如 https://localhost)，回环地址一律不可用。
 */
const isClientShell = (): boolean => {
  if (typeof window === 'undefined') return false;
  return (
    window.location.protocol === 'file:' ||
    Boolean((window as any).electronAPI?.isElectron) ||
    Boolean((window as any).Capacitor)
  );
};

export const formatMediaUrl = (url?: string): string => {
  if (!url) return '';

  // 本地生成的数据源原样返回
  if (url.startsWith('data:') || url.startsWith('blob:')) return url;

  if (url.startsWith('http://') || url.startsWith('https://')) {
    try {
      const parsed = new URL(url);

      // 1. 壳体内回环地址必须改写为公网服务端，否则会被解析为手机 / 客户端自身
      if (
        isClientShell() &&
        (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1' || parsed.hostname === '0.0.0.0')
      ) {
        return `${SERVER_BASE}${parsed.pathname}${parsed.search}`;
      }

      // 2. 我方服务端统一升级为 HTTPS，规避壳体内混合内容 (Mixed Content) 拦截
      if (parsed.host === SERVER_HOST && parsed.protocol === 'http:') {
        parsed.protocol = 'https:';
        return parsed.toString();
      }
    } catch {}

    return url;
  }

  // 纯相对路径补齐服务端公网地址
  const cleanPath = url.startsWith('/') ? url : `/${url}`;
  return `${SERVER_BASE}${cleanPath}`;
};
