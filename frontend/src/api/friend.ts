import { apiClient, ApiResponse } from './client';

export interface FriendItem {
  user_id: string;
  username: string;
  avatar_url?: string;
  signature?: string;
  remark?: string;
}

export interface FriendRequestItem {
  user_id: string;
  username: string;
  remark?: string;
  created_at?: string;
}

export const friendApi = {
  // 获取好友列表
  getList: async () => {
    const res = await apiClient.get<ApiResponse<{ friends: FriendItem[] }>>('/friends');
    return res.data;
  },

  // 获取待处理的好友申请
  getPendingRequests: async () => {
    const res = await apiClient.get<ApiResponse<{ requests: FriendRequestItem[] }>>('/friends/pending');
    return res.data;
  },

  // 发起好友申请
  apply: async (targetUsername: string, remark: string = '') => {
    const res = await apiClient.post<ApiResponse<null>>('/friends/apply', {
      target_username: targetUsername,
      remark,
    });
    return res.data;
  },

  // 审批好友申请 (accept | reject)
  respond: async (targetUserId: string, action: 'accept' | 'reject') => {
    const res = await apiClient.post<ApiResponse<null>>('/friends/respond', {
      target_user_id: targetUserId,
      action,
    });
    return res.data;
  },

  // 解除好友关系
  deleteFriend: async (friendId: string) => {
    const res = await apiClient.delete<ApiResponse<null>>(`/friends/${friendId}`);
    return res.data;
  },
};
