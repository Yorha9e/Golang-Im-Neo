import { apiClient, ApiResponse } from './client';

export interface PostCardPreview {
  post_id: string;
  title: string;
  cover_url: string;
  target_url: string;
  description: string;
}

export interface PostItem {
  id: string;
  user_id: string;
  username: string;
  media_type: 'text' | 'image' | 'video' | 'bilibili';
  content: string;
  media_url?: string;
  bilibili_bvid?: string;
  created_at: string;
  card?: PostCardPreview;
}

export interface UserProfile {
  user_id: string;
  username: string;
  nickname?: string;
  signature?: string;
  avatar_url?: string;
  created_at?: string;
  posts?: PostItem[];
}

export const profileApi = {
  // 公开查看用户主页
  getProfile: async (username: string) => {
    const res = await apiClient.get<ApiResponse<UserProfile>>(`/profile/${username}`);
    return res.data;
  },

  // 更新个人资料
  updateProfile: async (data: { nickname?: string; signature?: string; avatar_url?: string }) => {
    const res = await apiClient.put<ApiResponse<null>>('/profile', data);
    return res.data;
  },

  // 发布动态
  createPost: async (data: {
    media_type: 'text' | 'image' | 'video' | 'bilibili';
    content: string;
    media_url?: string;
    bilibili_bvid?: string;
  }) => {
    const res = await apiClient.post<ApiResponse<PostItem>>('/profile/posts', data);
    return res.data;
  },

  // 删除动态
  deletePost: async (postId: string) => {
    const res = await apiClient.delete<ApiResponse<null>>(`/profile/posts/${postId}`);
    return res.data;
  },

  // 获取动态外链卡片
  getPostCard: async (postId: string) => {
    const res = await apiClient.get<ApiResponse<PostCardPreview>>(`/posts/${postId}/card`);
    return res.data;
  },
};
