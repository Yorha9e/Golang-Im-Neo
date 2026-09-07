import { create } from 'zustand';
import { friendApi, FriendItem, FriendRequestItem, groupApi, GroupItem } from '../api';

export interface FriendState {
  friends: FriendItem[];
  pendingRequests: FriendRequestItem[];
  groups: GroupItem[];
  isLoading: boolean;

  fetchFriends: () => Promise<void>;
  fetchPendingRequests: () => Promise<void>;
  fetchGroups: () => Promise<void>;
  fetchAll: () => Promise<void>;
}

export const useFriendStore = create<FriendState>((set, get) => ({
  friends: [],
  pendingRequests: [],
  groups: [],
  isLoading: false,

  fetchFriends: async () => {
    try {
      const res = await friendApi.getList();
      if (res.code === 0 && res.data.friends) {
        set({ friends: res.data.friends });
      }
    } catch (err) {
      console.error('拉取好友列表失败:', err);
    }
  },

  fetchPendingRequests: async () => {
    try {
      const res = await friendApi.getPendingRequests();
      if (res.code === 0 && res.data.requests) {
        set({ pendingRequests: res.data.requests });
      }
    } catch (err) {
      console.error('拉取待处理申请失败:', err);
    }
  },

  fetchGroups: async () => {
    try {
      const res = await groupApi.getMyGroups();
      if (res.code === 0 && res.data.groups) {
        set({ groups: res.data.groups });
      }
    } catch (err) {
      console.error('拉取群列表失败:', err);
    }
  },

  fetchAll: async () => {
    set({ isLoading: true });
    await Promise.allSettled([
      get().fetchFriends(),
      get().fetchPendingRequests(),
      get().fetchGroups(),
    ]);
    set({ isLoading: false });
  },
}));
