import React, { useState, useRef } from 'react';
import { motion } from 'framer-motion';
import { useChatStore } from '../../store';
import { mediaApi } from '../../api';
import {
  Send,
  Image as ImageIcon,
  Mic,
  PhoneCall,
  Loader2,
} from 'lucide-react';

interface ChatInputProps {
  onOpenVoiceRecord: () => void;
  onStartCall?: () => void;
  isPrivateChat?: boolean;
}

export const ChatInput: React.FC<ChatInputProps> = ({
  onOpenVoiceRecord,
  onStartCall,
  isPrivateChat = false,
}) => {
  const [text, setText] = useState('');
  const [isUploading, setIsUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const { sendTextMessage, sendMediaMessage } = useChatStore();

  const handleSend = async () => {
    if (!text.trim() || isUploading) return;
    const content = text.trim();
    setText('');
    try {
      await sendTextMessage(content);
    } catch (err) {
      console.error('发送消息异常:', err);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  // 选择并上传图片或视频
  const handleMediaSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    // 区分图片与视频
    const isVideo = file.type.startsWith('video/') || /\.(mp4|mov|webm|mkv|avi)$/i.test(file.name);
    const mediaType: 'image' | 'video' = isVideo ? 'video' : 'image';

    setIsUploading(true);
    try {
      const res = await mediaApi.upload(file, mediaType, 'public');
      if (res.code === 0 && res.data) {
        const mediaUrl = res.data.access_url || res.data.url;
        if (mediaUrl) {
          await sendMediaMessage(mediaUrl, mediaType, {
            mid: res.data.mid,
            filename: file.name,
          });
        }
      } else {
        alert(res.msg || '上传失败');
      }
    } catch (err: any) {
      console.error('上传多媒体失败:', err);
      alert(err.message || '上传失败，请检查网络');
    } finally {
      setIsUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  return (
    <div className="p-4 glass-panel border-t border-[#C9B99A]/30 shrink-0">
      <div className="flex flex-col rounded-2xl glass-input p-2.5 shadow-warm-sm focus-within:border-[#8B7355] transition-all">
        {/* 输入框本体 */}
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="输入消息，Enter 发送，Shift+Enter 换行..."
          rows={2}
          className="w-full bg-transparent resize-none outline-none text-sm text-[#2E2419] placeholder-[#A3927C] px-1.5"
        />

        {/* 底部工具条与发送按钮 */}
        <div className="flex items-center justify-between pt-2 border-t border-[#C9B99A]/20 mt-1">
          {/* 左侧多媒体拓展 */}
          <div className="flex items-center gap-1.5">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*,video/*"
              className="hidden"
              onChange={handleMediaSelect}
            />

            {/* 发送图片与视频 */}
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={() => fileInputRef.current?.click()}
              disabled={isUploading}
              title="发送图片或视频"
              className="p-1.5 rounded-xl text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
            >
              {isUploading ? (
                <Loader2 size={18} className="animate-spin text-[#8B7355]" />
              ) : (
                <ImageIcon size={18} />
              )}
            </motion.button>

            {/* 录制语音 */}
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={onOpenVoiceRecord}
              title="录制语音消息"
              className="p-1.5 rounded-xl text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
            >
              <Mic size={18} />
            </motion.button>

            {/* 实时语音通话 (仅单聊) */}
            {isPrivateChat && (
              <motion.button
                whileTap={{ scale: 0.9 }}
                onClick={onStartCall}
                title="发起实时语音通话"
                className="p-1.5 rounded-xl text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
              >
                <PhoneCall size={18} />
              </motion.button>
            )}
          </div>

          {/* 右侧发送按钮 */}
          <motion.button
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.92 }}
            onClick={handleSend}
            disabled={!text.trim() || isUploading}
            className="py-1.5 px-4 rounded-xl bg-gradient-to-r from-[#8B7355] to-[#7A6348] text-[#FAF8F5] text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-40 transition-all"
          >
            <span>发送</span>
            <Send size={13} />
          </motion.button>
        </div>
      </div>
    </div>
  );
};
