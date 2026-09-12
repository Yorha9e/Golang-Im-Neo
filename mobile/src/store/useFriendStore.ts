import { create } from 'zustand';
import { friendApi, FriendItem, FriendRequestItem, groupApi, GroupItem } from '../api';

export interface FriendState {
  friends: FriendItem[];
  pendingRequests: FriendRequestItem[];
  groups: GroupItem[];
  isLoading: boolean;
  isRefreshing: boolean;

  fetchFriends: () => Promise<void>;
  fetchPendingRequests: () => Promise<void>;
  fetchGroups: () => Promise<void>;
  fetchAll: (silent?: boolean) => Promise<void>;
  removeFriend: (friendId: string) => Promise<boolean>;
}

export const useFriendStore = create<FriendState>((set, get) => ({
  friends: [],
  pendingRequests: [],
  groups: [],
  isLoading: false,
  isRefreshing: false,

  fetchFriends: async () => {
    try {
      const res = await friendApi.getList();
      if (res && res.code === 0) {
        const rawFriends = res.data?.friends || (Array.isArray(res.data) ? res.data : []);
        set({ friends: Array.isArray(rawFriends) ? rawFriends : [] });
      }
    } catch (err) {
      console.error('拉取好友列表失败:', err);
    }
  },

  fetchPendingRequests: async () => {
    try {
      const res = await friendApi.getPendingRequests();
      if (res && res.code === 0) {
        const rawRequests = res.data?.requests || (Array.isArray(res.data) ? res.data : []);
        set({ pendingRequests: Array.isArray(rawRequests) ? rawRequests : [] });
      }
    } catch (err) {
      console.error('拉取待处理申请失败:', err);
    }
  },

  fetchGroups: async () => {
    try {
      const res = await groupApi.getMyGroups();
      if (res && res.code === 0) {
        const rawGroups = res.data?.groups || (Array.isArray(res.data) ? res.data : []);
        set({ groups: Array.isArray(rawGroups) ? rawGroups : [] });
      }
    } catch (err) {
      console.error('拉取群列表失败:', err);
    }
  },

  fetchAll: async (silent = false) => {
    if (!silent) {
      set({ isRefreshing: true });
    }
    try {
      await Promise.allSettled([
        get().fetchFriends(),
        get().fetchPendingRequests(),
        get().fetchGroups(),
      ]);
    } finally {
      if (!silent) {
        set({ isRefreshing: false });
      }
    }
  },

  removeFriend: async (friendId: string) => {
    try {
      const res = await friendApi.deleteFriend(friendId);
      if (res && res.code === 0) {
        set((state) => ({
          friends: state.friends.filter((f) => f.user_id !== friendId),
        }));
        return true;
      }
      return false;
    } catch (err) {
      console.error('删除好友失败:', err);
      return false;
    }
  },
}));
