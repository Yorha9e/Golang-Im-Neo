import { apiClient, ApiResponse } from './client';

export interface HistoryMessageItem {
  id?: string;
  seq: number;
  from_uid: string;
  to_uid: string;
  content: string;
  timestamp: number;
  extra?: string;
  stanza_id?: string;
}

export const messageApi = {
  // 获取单聊历史消息
  getHistory: async (covId: string, sinceSeq: number = 0, limit: number = 50) => {
    const res = await apiClient.get<ApiResponse<{ messages: HistoryMessageItem[] }>>('/messages/history', {
      params: {
        cov_id: covId,
        since_seq: sinceSeq,
        limit,
      },
    });
    return res.data;
  },
};

export const adminApi = {
  // 封禁用户
  banUser: async (userId: string) => {
    const res = await apiClient.post<ApiResponse<null>>(`/admin/users/${userId}/ban`);
    return res.data;
  },

  // 精准踢掉单个会话
  kickSession: async (sessionId: string) => {
    const res = await apiClient.post<ApiResponse<null>>(`/admin/sessions/${sessionId}/kick`);
    return res.data;
  },

  // 发送全服系统广播
  broadcast: async (content: string) => {
    const res = await apiClient.post<ApiResponse<null>>('/admin/broadcast', { content });
    return res.data;
  },
};
