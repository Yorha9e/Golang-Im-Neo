import { create } from 'zustand';
import { authApi, LoginReq, RegisterReq, tokenStorage, parseJwt, profileApi } from '../api';
import { wsClient } from '../socket/wsClient';

export interface UserState {
  isAuthenticated: boolean;
  userId: string;
  username: string;
  role: string;
  sessionId: string;
  accessToken: string | null;
  avatarUrl: string;
  nickname: string;
  
  // 操作方法
  initAuth: () => void;
  login: (data: LoginReq) => Promise<void>;
  register: (data: RegisterReq) => Promise<void>;
  logout: () => void;
  setAvatarUrl: (url: string) => void;
  setNickname: (name: string) => void;
  fetchMyProfile: () => Promise<void>;
}

export const useAuthStore = create<UserState>((set, get) => ({
  isAuthenticated: false,
  userId: '',
  username: '',
  role: 'user',
  sessionId: '',
  accessToken: null,
  avatarUrl: '',
  nickname: '',

  setAvatarUrl: (url: string) => {
    tokenStorage.setAvatar(url);
    set({ avatarUrl: url });
  },

  setNickname: (name: string) => {
    tokenStorage.setNickname(name);
    set({ nickname: name });
  },

  fetchMyProfile: async () => {
    const uname = get().username;
    if (!uname) return;
    try {
      const res = await profileApi.getProfile(uname);
      if (res.code === 0 && res.data) {
        const u = res.data.user || res.data;
        const av = u.avatar_url || '';
        const nk = u.nickname || '';
        tokenStorage.setAvatar(av);
        tokenStorage.setNickname(nk);
        set({ avatarUrl: av, nickname: nk });
      }
    } catch (e) {
      console.warn('拉取个人资料头像失败:', e);
    }
  },

  initAuth: () => {
    const token = tokenStorage.getAccessToken();
    const userId = tokenStorage.getUserId();
    const username = tokenStorage.getUsername();
    const sessionId = tokenStorage.getSessionId();
    let role = tokenStorage.getRole();
    const avatar = tokenStorage.getAvatar();
    const nickname = tokenStorage.getNickname();

    if (token && userId) {
      const payload = parseJwt(token);
      if (payload && payload.role) {
        role = payload.role;
      } else if (username === 'superadmin') {
        role = 'superadmin';
      }

      set({
        isAuthenticated: true,
        userId: userId,
        username: username || 'User',
        role: role,
        sessionId: sessionId || '',
        accessToken: token,
        avatarUrl: avatar,
        nickname: nickname,
      });

      // 触发 WS 自动连接并拉取最新个人资料
      wsClient.connect();
      get().fetchMyProfile();
    }
  },

  login: async (data: LoginReq) => {
    const res = await authApi.login(data);
    if (res.code === 0 && res.data.access_token) {
      const token = res.data.access_token;
      const payload = parseJwt(token);
      
      let userRole = res.data.role || payload?.role;
      if (!userRole && data.username === 'superadmin') {
        userRole = 'superadmin';
      }
      userRole = userRole || 'user';

      const authData = {
        access_token: token,
        refresh_token: res.data.refresh_token,
        user_id: res.data.user_id,
        session_id: res.data.session_id,
        username: data.username,
        role: userRole,
      };
      tokenStorage.setAuth(authData);

      set({
        isAuthenticated: true,
        userId: res.data.user_id,
        username: data.username,
        role: userRole,
        sessionId: res.data.session_id,
        accessToken: token,
      });

      // 建立长连接并拉取头像
      wsClient.connect();
      get().fetchMyProfile();
    } else {
      throw new Error(res.msg || '登录失败');
    }
  },

  register: async (data: RegisterReq) => {
    const res = await authApi.register(data);
    if (res.code === 0) {
      const loginRes = await authApi.login({ username: data.username, password: data.password });
      if (loginRes.code === 0 && loginRes.data.access_token) {
        const token = loginRes.data.access_token;
        const payload = parseJwt(token);
        const userRole = loginRes.data.role || payload?.role || 'user';

        tokenStorage.setAuth({
          access_token: token,
          refresh_token: loginRes.data.refresh_token,
          user_id: loginRes.data.user_id,
          session_id: loginRes.data.session_id,
          username: data.username,
          role: userRole,
        });

        set({
          isAuthenticated: true,
          userId: loginRes.data.user_id,
          username: data.username,
          role: userRole,
          sessionId: loginRes.data.session_id,
          accessToken: token,
        });

        wsClient.connect();
        get().fetchMyProfile();
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
      avatarUrl: '',
      nickname: '',
    });
  },
}));
