import React, { useState } from 'react';
import {
  useChatStore,
  useAudioStore,
} from '../../store';
import { webRtcEngine } from '../../socket/webrtc';
import { MessageList } from './MessageList';
import { ChatInput } from './ChatInput';
import {
  Hash,
  User,
  Users,
  PhoneCall,
  X,
} from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

interface ChatWindowProps {
  onOpenVoiceRecord: () => void;
  onOpenGroupManager: (groupId: string) => void;
  onOpenProfile?: (username: string) => void;
}

export const ChatWindow: React.FC<ChatWindowProps> = ({
  onOpenVoiceRecord,
  onOpenGroupManager,
  onOpenProfile,
}) => {
  const { activeConversation, messages, loadHistory } = useChatStore();
  const [previewImageUrl, setPreviewImageUrl] = useState<string | null>(null);

  const currentMessages = messages[activeConversation.id] || [];

  const handleStartCall = () => {
    if (activeConversation.type === 'private') {
      webRtcEngine.call(activeConversation.id, activeConversation.name);
    }
  };

  return (
    <div className="flex-1 flex flex-col h-full bg-[#FAF8F5] relative overflow-hidden">
      {/* 视窗顶栏 */}
      <div className="h-14 px-6 flex items-center justify-between glass-panel border-b border-[#C9B99A]/30 z-10 shrink-0">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 flex items-center justify-center text-[#8B7355] font-bold text-sm shadow-warm-sm">
            {activeConversation.type === 'hall' && <Hash size={18} />}
            {activeConversation.type === 'private' && <User size={18} />}
            {activeConversation.type === 'group' && <Users size={18} />}
          </div>
          <div>
            <h2 className="font-bold text-sm text-[#2E2419] flex items-center gap-2">
              {activeConversation.name}
              {activeConversation.type === 'group' && (
                <span className="text-[10px] px-1.5 py-0.5 rounded font-mono bg-[#8B7355]/15 text-[#8B7355]">
                  群聊
                </span>
              )}
            </h2>
            <p className="text-[11px] text-[#A3927C] leading-none mt-0.5">
              {activeConversation.type === 'hall'
                ? '全服公共频段 · 畅所欲言'
                : activeConversation.type === 'private'
                ? '双向安全加密私聊会话'
                : '群组成员协同交流频道'}
            </p>
          </div>
        </div>

        {/* 顶部操作区 */}
        <div className="flex items-center gap-2">
          {activeConversation.type === 'private' && (
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={handleStartCall}
              className="flex items-center gap-1 px-3 py-1.5 rounded-xl bg-[#F0EBE3] hover:bg-[#EAE4DC] border border-[#C9B99A]/40 text-xs font-semibold text-[#8B7355] shadow-warm-sm transition-all"
            >
              <PhoneCall size={14} />
              <span>语音通话</span>
            </motion.button>
          )}

          {activeConversation.type === 'group' && (
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={() => onOpenGroupManager(activeConversation.id)}
              className="flex items-center gap-1 px-3 py-1.5 rounded-xl bg-[#F0EBE3] hover:bg-[#EAE4DC] border border-[#C9B99A]/40 text-xs font-semibold text-[#8B7355] shadow-warm-sm transition-all"
            >
              <Users size={14} />
              <span>群成员与管理</span>
            </motion.button>
          )}
        </div>
      </div>

      {/* 消息主体滚动流 */}
      <MessageList
        messages={currentMessages}
        conversation={activeConversation}
        onLoadMoreHistory={() => loadHistory(activeConversation.id)}
        onPreviewImage={(url) => setPreviewImageUrl(url)}
        onOpenProfile={onOpenProfile}
      />

      {/* 底部输入框 */}
      <ChatInput
        onOpenVoiceRecord={onOpenVoiceRecord}
        onStartCall={handleStartCall}
        isPrivateChat={activeConversation.type === 'private'}
      />

      {/* 图片放大预览模态框 */}
      <AnimatePresence>
        {previewImageUrl && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => setPreviewImageUrl(null)}
            className="fixed inset-0 z-50 flex items-center justify-center p-6 bg-black/60 backdrop-blur-md cursor-zoom-out"
          >
            <motion.div
              initial={{ scale: 0.9 }}
              animate={{ scale: 1 }}
              exit={{ scale: 0.9 }}
              className="relative max-w-4xl max-h-[85vh] rounded-2xl overflow-hidden glass-card shadow-warm-lg"
              onClick={(e) => e.stopPropagation()}
            >
              <img
                src={previewImageUrl}
                alt="预览放大图"
                className="w-full h-full object-contain max-h-[85vh]"
              />
              <button
                onClick={() => setPreviewImageUrl(null)}
                className="absolute top-3 right-3 p-2 rounded-full bg-black/50 text-white hover:bg-black/70 transition-colors"
              >
                <X size={18} />
              </button>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};
