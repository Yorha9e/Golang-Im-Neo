import { apiClient, ApiResponse } from './client';

export interface GroupItem {
  id: string;
  group_id?: string;
  name: string;
  avatar_url?: string;
  announcement?: string;
  notice?: string;
  owner_id?: string;
  member_count?: number;
  max_members?: number;
  created_at?: string;
}

export interface GroupMemberItem {
  user_id: string;
  username: string;
  avatar_url?: string;
  role: 'owner' | 'admin' | 'member';
  muted?: boolean;
  muted_at?: number;
  joined_at?: string;
}

export const groupApi = {
  // 创建群聊 (POST /groups)
  createGroup: async (data: { name: string; avatar_url?: string; notice?: string }) => {
    const res = await apiClient.post<ApiResponse<GroupItem>>('/groups', {
      name: data.name,
    });
    if (res.data && res.data.data) {
      const g = res.data.data;
      g.id = g.group_id || g.id;
    }
    return res.data;
  },

  // 获取我加入的群列表 (GET /groups -> data: GroupItem[])
  getMyGroups: async () => {
    const res = await apiClient.get<ApiResponse<GroupItem[] | { groups: GroupItem[] }>>('/groups');
    if (res.data && res.data.data) {
      let list: GroupItem[] = [];
      if (Array.isArray(res.data.data)) {
        list = res.data.data;
      } else if ((res.data.data as any).groups) {
        list = (res.data.data as any).groups;
      }
      // 统一规格化 id
      list.forEach((g) => {
        g.id = g.group_id || g.id;
        g.notice = g.announcement || g.notice || '';
      });
      return { code: 0, msg: 'success', data: { groups: list } };
    }
    return { code: 0, msg: 'success', data: { groups: [] } };
  },

  // 获取群详情 (GET /groups/:id)
  getGroupDetail: async (groupId: string) => {
    const res = await apiClient.get<ApiResponse<GroupItem>>(`/groups/${groupId}`);
    if (res.data && res.data.data) {
      const g = res.data.data;
      g.id = g.group_id || g.id;
      g.notice = g.announcement || g.notice || '';
    }
    return res.data;
  },

  // 获取群成员列表 (GET /groups/:id/members -> data: GroupMemberItem[])
  getGroupMembers: async (groupId: string) => {
    const res = await apiClient.get<ApiResponse<GroupMemberItem[] | { members: GroupMemberItem[] }>>(`/groups/${groupId}/members`);
    if (res.data && res.data.data) {
      let list: GroupMemberItem[] = [];
      if (Array.isArray(res.data.data)) {
        list = res.data.data;
      } else if ((res.data.data as any).members) {
        list = (res.data.data as any).members;
      }
      return { code: 0, msg: 'success', data: { members: list } };
    }
    return { code: 0, msg: 'success', data: { members: [] } };
  },

  // 邀请好友加入群聊 (POST /groups/:id/invite -> body: { user_ids: [uid] })
  addMember: async (groupId: string, userId: string) => {
    const res = await apiClient.post<ApiResponse<any>>(`/groups/${groupId}/invite`, {
      user_ids: [userId],
    });
    return res.data;
  },

  // 退出群聊 (POST /groups/:id/leave)
  leaveGroup: async (groupId: string) => {
    const res = await apiClient.post<ApiResponse<null>>(`/groups/${groupId}/leave`);
    return res.data;
  },

  // 踢出群成员 (DELETE /groups/:id/members/:uid)
  kickMember: async (groupId: string, userId: string) => {
    const res = await apiClient.delete<ApiResponse<null>>(`/groups/${groupId}/members/${userId}`);
    return res.data;
  },

  // 群内禁言/解禁 (PATCH /groups/:id/members/:uid/mute -> body: { muted: bool })
  muteMember: async (groupId: string, userId: string, muted: boolean) => {
    const res = await apiClient.patch<ApiResponse<null>>(`/groups/${groupId}/members/${userId}/mute`, {
      muted,
    });
    return res.data;
  },

  // 获取群历史消息漫游 (GET /groups/:id/history)
  getGroupHistory: async (groupId: string, beforeSeq: number = 0, limit: number = 50) => {
    const res = await apiClient.get<ApiResponse<{ messages: any[]; has_more: boolean }>>(`/groups/${groupId}/history`, {
      params: { before_seq: beforeSeq, limit },
    });
    return res.data;
  },
};
