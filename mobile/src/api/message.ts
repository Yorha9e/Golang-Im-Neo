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
  content_type?: number;
}

export interface HistoryResp {
  messages: HistoryMessageItem[];
  has_more: boolean;
}

export const messageApi = {
  // 1. 统一历史漫游 (通过 cov_id)
  getHistory: async (covId: string, beforeSeq: number = 0, limit: number = 50) => {
    const res = await apiClient.get<ApiResponse<HistoryResp>>('/messages/history', {
      params: {
        cov_id: covId,
        before_seq: beforeSeq,
        limit,
      },
    });
    return res.data;
  },

  // 2. 大厅历史消息快捷接口 (GET /messages/hall/history)
  getHallHistory: async (beforeSeq: number = 0, limit: number = 50) => {
    const res = await apiClient.get<ApiResponse<HistoryResp>>('/messages/hall/history', {
      params: {
        before_seq: beforeSeq,
        limit,
      },
    });
    return res.data;
  },

  // 3. 私聊历史消息快捷接口 (GET /messages/private/:target_user_id/history)
  getPrivateHistory: async (targetUserId: string, beforeSeq: number = 0, limit: number = 50) => {
    const res = await apiClient.get<ApiResponse<HistoryResp>>(`/messages/private/${targetUserId}/history`, {
      params: {
        before_seq: beforeSeq,
        limit,
      },
    });
    return res.data;
  },
};
