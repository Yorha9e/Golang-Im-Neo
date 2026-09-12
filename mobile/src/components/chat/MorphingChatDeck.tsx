import React, { useState } from 'react';
import { motion, AnimatePresence, useMotionValue, useTransform } from 'framer-motion';
import {
  useChatStore,
  Conversation
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
  ChevronLeft,
  Flame,
  ArrowLeftRight,
  Sparkles,
  Info
} from 'lucide-react';
import { formatMediaUrl } from '../../utils/media';
import { soundEffects } from '../../audio/soundEffects';

interface MorphingChatDeckProps {
  onBack: () => void;
  onOpenVoiceRecord: () => void;
  onOpenGroupManager: (groupId: string) => void;
  onOpenProfile: (username: string) => void;
  onSwitchConversation?: (conv: Conversation) => void;
}

export const MorphingChatDeck: React.FC<MorphingChatDeckProps> = ({
  onBack,
  onOpenVoiceRecord,
  onOpenGroupManager,
  onOpenProfile,
  onSwitchConversation,
}) => {
  const { activeConversation, conversations, messages, loadHistory } = useChatStore();
  const [previewImageUrl, setPreviewImageUrl] = useState<string | null>(null);

  const currentMessages = messages[activeConversation.id] || [];

  // 物理手势位移追踪（支持右滑退出）
  const dragX = useMotionValue(0);
  const opacity = useTransform(dragX, [0, 150], [1, 0.7]);
  const scale = useTransform(dragX, [0, 150], [1, 0.96]);

  const handleStartCall = () => {
    if (activeConversation.type === 'private') {
      soundEffects.playHapticTick();
      webRtcEngine.call(activeConversation.id, activeConversation.name);
    }
  };

  // 快速切向全服公共大厅 (无需退出)
  const handleQuickSwitchHall = () => {
    soundEffects.playHapticTick();
    if (activeConversation.id !== 'hall') {
      onSwitchConversation?.({
        id: 'hall',
        type: 'hall',
        name: '全服公共大厅',
      });
    }
  };

  return (
    <motion.div
      layoutId={`chat-deck-${activeConversation.id}`}
      style={{ opacity, scale }}
      drag="x"
      dragConstraints={{ left: 0, right: 0 }}
      dragElastic={{ right: 0.7, left: 0.05 }}
      onDragEnd={(_, info) => {
        // 向右滑动手势速度或位移达到阈值时触发自然返回
        if (info.offset.x > 110 || info.velocity.x > 500) {
          soundEffects.playHapticTick();
          onBack();
        }
      }}
      className="fixed inset-0 z-40 flex flex-col bg-[#FAF8F5] text-[#2E2419] overflow-hidden select-none"
    >
      {/* 1. 顶部手势引导小把手 (Pull to dismiss indicator) */}
      <div className="w-full flex justify-center pt-2 pb-1 bg-[#FAF8F5]">
        <div className="w-10 h-1 rounded-full bg-[#C9B99A]/50" />
      </div>

      {/* 2. 悬浮灵动 Header 导航栏 */}
      <div className="h-14 px-3 flex items-center justify-between bg-[#FAF8F5]/90 backdrop-blur-xl border-b border-[#C9B99A]/30 shrink-0 z-10">
        {/* 左侧返回与会话详情 */}
        <div className="flex items-center gap-2 min-w-0 flex-1 mr-2">
          {/* 原生触控返回大拇指按钮 */}
          <motion.button
            whileTap={{ scale: 0.88 }}
            onClick={() => {
              soundEffects.playHapticTick();
              onBack();
            }}
            className="p-2 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-[#8B7355] hover:bg-[#EAE4DC] active:bg-[#C9B99A]/30 transition-colors shrink-0 shadow-warm-xs"
            title="返回引力场 (或向右轻划屏幕)"
          >
            <ChevronLeft size={20} />
          </motion.button>

          {/* 会话图标/头像 */}
          <div
            onClick={() => {
              if (activeConversation.type === 'private') {
                onOpenProfile(activeConversation.name);
              }
            }}
            className="w-10 h-10 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 flex items-center justify-center text-[#8B7355] font-bold text-sm shadow-warm-sm shrink-0 overflow-hidden cursor-pointer"
          >
            {activeConversation.avatar_url ? (
              <img
                src={formatMediaUrl(activeConversation.avatar_url)}
                alt={activeConversation.name}
                className="w-full h-full object-cover"
              />
            ) : activeConversation.type === 'hall' ? (
              <Flame size={20} className="text-amber-500 animate-pulse" />
            ) : activeConversation.type === 'private' ? (
              <User size={20} />
            ) : (
              <Users size={20} />
            )}
          </div>

          {/* 会话名称与在线胶囊 */}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5 truncate">
              <h2 className="font-bold text-xs sm:text-sm text-[#2E2419] truncate">
                {activeConversation.name}
              </h2>
              {activeConversation.type === 'group' && (
                <span className="text-[9px] px-1.5 py-0.2 rounded-md font-mono bg-[#8B7355]/15 text-[#8B7355] shrink-0">
                  群
                </span>
              )}
            </div>

            <p className="text-[10px] text-[#A3927C] truncate font-medium flex items-center gap-1">
              {activeConversation.type === 'hall' ? (
                <>
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                  <span>全服公共频段 · 畅所欲言</span>
                </>
              ) : activeConversation.type === 'private' ? (
                <>
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                  <span>双向安全点对点</span>
                </>
              ) : (
                '多人协同共鸣星团'
              )}
            </p>
          </div>
        </div>

        {/* 右侧操作区 */}
        <div className="flex items-center gap-1.5 shrink-0">
          {/* 快捷切向大厅胶囊 (仅当不是大厅时显示) */}
          {activeConversation.id !== 'hall' && (
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={handleQuickSwitchHall}
              className="px-2.5 py-1.5 rounded-xl bg-amber-500/10 border border-amber-300/40 text-amber-800 text-[11px] font-semibold flex items-center gap-1 shadow-warm-xs hover:bg-amber-500/20 transition-colors"
              title="一键切向全服公共大厅"
            >
              <Flame size={13} className="text-amber-600" />
              <span>大厅</span>
            </motion.button>
          )}

          {/* 私聊专有：语音通话与资料 */}
          {activeConversation.type === 'private' && (
            <>
              <motion.button
                whileTap={{ scale: 0.9 }}
                onClick={handleStartCall}
                className="p-2 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-[#8B7355] shadow-warm-xs hover:bg-[#EAE4DC] transition-colors"
                title="发起 WebRTC 实时加密语音"
              >
                <PhoneCall size={16} />
              </motion.button>

              <motion.button
                whileTap={{ scale: 0.9 }}
                onClick={() => onOpenProfile(activeConversation.name)}
                className="p-2 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-[#8B7355] shadow-warm-xs hover:bg-[#EAE4DC] transition-colors"
                title="查看用户主页"
              >
                <Info size={16} />
              </motion.button>
            </>
          )}

          {/* 群聊专有：群成员管理 */}
          {activeConversation.type === 'group' && (
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={() => onOpenGroupManager(activeConversation.id)}
              className="px-2.5 py-1.5 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-xs font-semibold text-[#8B7355] shadow-warm-xs hover:bg-[#EAE4DC] transition-colors flex items-center gap-1"
            >
              <Users size={14} />
              <span>管理</span>
            </motion.button>
          )}
        </div>
      </div>

      {/* 3. 消息滚动流 */}
      <div className="flex-1 min-h-0 flex flex-col relative">
        <MessageList
          messages={currentMessages}
          conversation={activeConversation}
          onLoadMoreHistory={() => loadHistory(activeConversation.id)}
          onPreviewImage={(url) => setPreviewImageUrl(url)}
          onOpenProfile={onOpenProfile}
        />
      </div>

      {/* 4. 底部聊天输入栏 */}
      <div className="shrink-0 bg-[#FAF8F5] pb-safe">
        <ChatInput
          onOpenVoiceRecord={onOpenVoiceRecord}
          onStartCall={handleStartCall}
          isPrivateChat={activeConversation.type === 'private'}
        />
      </div>

      {/* 5. 图片全屏灯箱预览 */}
      <AnimatePresence>
        {previewImageUrl && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => setPreviewImageUrl(null)}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-md cursor-zoom-out"
          >
            <motion.div
              initial={{ scale: 0.85 }}
              animate={{ scale: 1 }}
              exit={{ scale: 0.85 }}
              className="relative max-w-full max-h-[85vh] rounded-3xl overflow-hidden glass-card shadow-warm-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <img
                src={formatMediaUrl(previewImageUrl)}
                alt="放大预览图"
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
    </motion.div>
  );
};
