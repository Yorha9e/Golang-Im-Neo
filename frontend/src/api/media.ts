import { apiClient, ApiResponse } from './client';

export interface UploadMediaResp {
  mid: string;
  access_url: string;
  url?: string;
  media_type?: string;
  access_level?: string;
  file_size?: number;
  file_ext?: string;
  sha256?: string;
}

export const mediaApi = {
  // 上传多媒体文件 (头像/图片/语音/视频)
  // 严格对齐后端字段: file, media_type, access_level
  upload: async (
    file: File | Blob,
    type: 'avatar' | 'image' | 'voice' | 'video' = 'image',
    accessLevel: 'public' | 'private' = 'public',
    filename?: string
  ) => {
    const formData = new FormData();
    if (file instanceof Blob && !(file instanceof File) && filename) {
      formData.append('file', file, filename);
    } else {
      formData.append('file', file);
    }
    formData.append('media_type', type);
    formData.append('access_level', accessLevel);

    const res = await apiClient.post<ApiResponse<UploadMediaResp>>('/media/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    });

    // 确保 url 与 access_url 均有值供前端组件便捷消费
    if (res.data && res.data.data) {
      const accessUrl = res.data.data.access_url || res.data.data.url || '';
      res.data.data.url = accessUrl;
      res.data.data.access_url = accessUrl;
    }

    return res.data;
  },
};
