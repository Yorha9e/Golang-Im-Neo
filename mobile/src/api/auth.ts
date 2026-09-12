import { apiClient, ApiResponse } from './client';

export interface RegisterReq {
  username: string;
  password: string;
}

export interface RegisterResp {
  user_id: string;
  username: string;
}

export interface LoginReq {
  username: string;
  password: string;
  device_class?: string;
  device_name?: string;
}

export interface LoginResp {
  access_token: string;
  refresh_token: string;
  user_id: string;
  session_id: string;
  role: string;
  username?: string;
}

export interface TicketResp {
  ticket: string;
}

export const authApi = {
  register: async (data: RegisterReq) => {
    const res = await apiClient.post<ApiResponse<RegisterResp>>('/auth/register', data);
    return res.data;
  },

  login: async (data: LoginReq) => {
    const res = await apiClient.post<ApiResponse<LoginResp>>('/auth/login', {
      device_class: 'interactive',
      device_name: 'Web Chrome',
      ...data,
    });
    return res.data;
  },

  refresh: async (refreshToken: string, sessionId: string) => {
    const res = await apiClient.post<ApiResponse<{ access_token: string }>>('/auth/refresh', {
      refresh_token: refreshToken,
      session_id: sessionId,
    });
    return res.data;
  },

  getTicket: async () => {
    const res = await apiClient.post<ApiResponse<TicketResp>>('/auth/ticket');
    return res.data;
  },
};
