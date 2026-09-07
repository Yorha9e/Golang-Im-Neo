import { create } from 'zustand';
import { authApi, LoginReq, RegisterReq, tokenStorage } from '../api';
import { wsClient } from '../socket/wsClient';

export interface UserState {
  isAuthenticated: boolean;
  userId: string;
  username: string;
  role: string;
  sessionId: string;
  accessToken: string | null;
  
  // 操作方法
  initAuth: () => void;
  login: (data: LoginReq) => Promise<void>;
  register: (data: RegisterReq) => Promise<void>;
  logout: () => void;
}

export const useAuthStore = create<UserState>((set) => ({
  isAuthenticated: false,
  userId: '',
  username: '',
  role: 'user',
  sessionId: '',
  accessToken: null,

  initAuth: () => {
    const token = tokenStorage.getAccessToken();
    const userId = tokenStorage.getUserId();
    const username = tokenStorage.getUsername();
    const sessionId = tokenStorage.getSessionId();

    if (token && userId) {
      set({
        isAuthenticated: true,
        userId: userId,
        username: username || 'User',
        sessionId: sessionId || '',
        accessToken: token,
      });
      // 触发 WS 自动连接
      wsClient.connect();
    }
  },

  login: async (data: LoginReq) => {
    const res = await authApi.login(data);
    if (res.code === 0 && res.data.access_token) {
      const authData = {
        access_token: res.data.access_token,
        refresh_token: res.data.refresh_token,
        user_id: res.data.user_id,
        session_id: res.data.session_id,
        username: data.username,
      };
      tokenStorage.setAuth(authData);

      set({
        isAuthenticated: true,
        userId: res.data.user_id,
        username: data.username,
        role: res.data.role || 'user',
        sessionId: res.data.session_id,
        accessToken: res.data.access_token,
      });

      // 建立长连接
      wsClient.connect();
    } else {
      throw new Error(res.msg || '登录失败');
    }
  },

  register: async (data: RegisterReq) => {
    const res = await authApi.register(data);
    if (res.code === 0) {
      // 注册后直接进行登录
      const loginRes = await authApi.login({ username: data.username, password: data.password });
      if (loginRes.code === 0 && loginRes.data.access_token) {
        tokenStorage.setAuth({
          access_token: loginRes.data.access_token,
          refresh_token: loginRes.data.refresh_token,
          user_id: loginRes.data.user_id,
          session_id: loginRes.data.session_id,
          username: data.username,
        });

        set({
          isAuthenticated: true,
          userId: loginRes.data.user_id,
          username: data.username,
          role: loginRes.data.role || 'user',
          sessionId: loginRes.data.session_id,
          accessToken: loginRes.data.access_token,
        });

        wsClient.connect();
      }
    } else {
      throw new Error(res.msg || '注册失败');
    }
  },

  logout: () => {
    tokenStorage.clear();
    wsClient.disconnect();
    set({
      isAuthenticated: false,
      userId: '',
      username: '',
      role: 'user',
      sessionId: '',
      accessToken: null,
    });
  },
}));
