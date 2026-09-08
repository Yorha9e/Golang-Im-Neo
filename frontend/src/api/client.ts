import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios';

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

const API_BASE_URL = '/api/v1';

export const apiClient = axios.create({
  baseURL: API_BASE_URL,
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// 解析 JWT Payload 获取用户身份与角色
export function parseJwt(token: string): any {
  try {
    const base64Url = token.split('.')[1];
    if (!base64Url) return null;
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(
      atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    );
    return JSON.parse(jsonPayload);
  } catch {
    return null;
  }
}

// 本地 Token 与用户资料存储操作
export const tokenStorage = {
  getAccessToken: () => localStorage.getItem('neo_access_token'),
  getRefreshToken: () => localStorage.getItem('neo_refresh_token'),
  getSessionId: () => localStorage.getItem('neo_session_id'),
  getUserId: () => localStorage.getItem('neo_user_id'),
  getUsername: () => localStorage.getItem('neo_username'),
  getRole: () => localStorage.getItem('neo_role') || 'user',
  getAvatar: () => localStorage.getItem('neo_avatar') || '',
  getNickname: () => localStorage.getItem('neo_nickname') || '',
  
  setAuth: (data: {
    access_token: string;
    refresh_token?: string;
    session_id?: string;
    user_id?: string;
    username?: string;
    role?: string;
    avatar_url?: string;
    nickname?: string;
  }) => {
    if (data.access_token) localStorage.setItem('neo_access_token', data.access_token);
    if (data.refresh_token) localStorage.setItem('neo_refresh_token', data.refresh_token);
    if (data.session_id) localStorage.setItem('neo_session_id', data.session_id);
    if (data.user_id) localStorage.setItem('neo_user_id', data.user_id);
    if (data.username) localStorage.setItem('neo_username', data.username);
    if (data.role) localStorage.setItem('neo_role', data.role);
    if (data.avatar_url !== undefined) localStorage.setItem('neo_avatar', data.avatar_url);
    if (data.nickname !== undefined) localStorage.setItem('neo_nickname', data.nickname);
  },
  
  setAvatar: (url: string) => localStorage.setItem('neo_avatar', url),
  setNickname: (name: string) => localStorage.setItem('neo_nickname', name),

  clear: () => {
    localStorage.removeItem('neo_access_token');
    localStorage.removeItem('neo_refresh_token');
    localStorage.removeItem('neo_session_id');
    localStorage.removeItem('neo_user_id');
    localStorage.removeItem('neo_username');
    localStorage.removeItem('neo_role');
    localStorage.removeItem('neo_avatar');
    localStorage.removeItem('neo_nickname');
  },
};

// 请求拦截器：自动注入 Bearer Token
apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const token = tokenStorage.getAccessToken();
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

// 并发 Token 刷新控制队列
let isRefreshing = false;
let failedQueue: Array<{
  resolve: (value?: unknown) => void;
  reject: (reason?: unknown) => void;
}> = [];

const processQueue = (error: any = null) => {
  failedQueue.forEach((promise) => {
    if (error) {
      promise.reject(error);
    } else {
      promise.resolve();
    }
  });
  failedQueue = [];
};

// 响应拦截器：自动无感刷新 Token 并重试
apiClient.interceptors.response.use(
  (response) => {
    const res = response.data as ApiResponse;
    if (res && res.code === 10002) {
      return handleUnauthorized(response.config);
    }
    return response;
  },
  async (error: AxiosError) => {
    const originalRequest = error.config;
    if (!originalRequest) return Promise.reject(error);

    if (error.response?.status === 401) {
      return handleUnauthorized(originalRequest);
    }
    return Promise.reject(error);
  }
);

// 执行 Token 刷新与请求重试
async function handleUnauthorized(originalRequest: any) {
  if (originalRequest._retry) {
    tokenStorage.clear();
    window.dispatchEvent(new CustomEvent('auth:expired'));
    return Promise.reject(new Error('Token refresh loop detected'));
  }

  if (isRefreshing) {
    return new Promise((resolve, reject) => {
      failedQueue.push({ resolve, reject });
    })
      .then(() => {
        const token = tokenStorage.getAccessToken();
        if (token && originalRequest.headers) {
          originalRequest.headers.Authorization = `Bearer ${token}`;
        }
        return apiClient(originalRequest);
      })
      .catch((err) => Promise.reject(err));
  }

  originalRequest._retry = true;
  isRefreshing = true;

  const refreshToken = tokenStorage.getRefreshToken();
  const sessionId = tokenStorage.getSessionId();

  if (!refreshToken || !sessionId) {
    tokenStorage.clear();
    isRefreshing = false;
    window.dispatchEvent(new CustomEvent('auth:expired'));
    return Promise.reject(new Error('No refresh token available'));
  }

  try {
    const { data } = await axios.post<ApiResponse<{ access_token: string }>>('/api/v1/auth/refresh', {
      refresh_token: refreshToken,
      session_id: sessionId,
    });

    if (data.code === 0 && data.data.access_token) {
      const newToken = data.data.access_token;
      tokenStorage.setAuth({ access_token: newToken });
      if (originalRequest.headers) {
        originalRequest.headers.Authorization = `Bearer ${newToken}`;
      }
      processQueue();
      return apiClient(originalRequest);
    } else {
      throw new Error(data.msg || 'Refresh token failed');
    }
  } catch (refreshErr) {
    processQueue(refreshErr);
    tokenStorage.clear();
    window.dispatchEvent(new CustomEvent('auth:expired'));
    return Promise.reject(refreshErr);
  } finally {
    isRefreshing = false;
  }
}
