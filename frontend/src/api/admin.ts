import { apiClient, ApiResponse } from './client';

export interface SystemStats {
  server: {
    uptime_seconds: number;
    start_time: string;
    go_version: string;
    num_goroutine: number;
    memory?: {
      alloc_bytes: number;
      total_alloc_bytes: number;
      sys_bytes: number;
      heap_alloc_bytes: number;
    };
  };
  gateway: {
    online_connections: number;
    shard_count: number;
  };
  store: {
    batch_queue_len: number;
    batch_queue_cap: number;
    db_open_conns: number;
    db_in_use_conns: number;
  };
  counts: {
    users_total: number;
    users_banned: number;
    groups_total: number;
    messages_total: number;
  };
}

export const adminApi = {
  // 获取全服实时性能监控指标看板 (GET /admin/stats)
  getStats: async () => {
    const res = await apiClient.get<ApiResponse<SystemStats>>('/admin/stats');
    return res.data;
  },

  // 封禁违规用户 (POST /admin/users/:user_id/ban)
  banUser: async (userId: string) => {
    const res = await apiClient.post<ApiResponse<null>>(`/admin/users/${userId}/ban`);
    return res.data;
  },

  // 精准踢掉单个会话 (POST /admin/sessions/:session_id/kick)
  kickSession: async (sessionId: string) => {
    const res = await apiClient.post<ApiResponse<null>>(`/admin/sessions/${sessionId}/kick`);
    return res.data;
  },

  // 发送全服系统广播 (POST /admin/broadcast)
  broadcast: async (content: string) => {
    const res = await apiClient.post<ApiResponse<null>>('/admin/broadcast', { content });
    return res.data;
  },
};
