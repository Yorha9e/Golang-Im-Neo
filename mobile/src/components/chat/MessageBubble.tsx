import React from 'react';
import { motion } from 'framer-motion';
import { ChatMessage, useAuthStore } from '../../store';
import { MsgType } from '../../proto/message';
import { Check, AlertCircle, Loader2, Radio, Sparkles } from 'lucide-react';
import { AudioBubble } from './AudioBubble';
import { RoleBadge } from '../common/RoleBadge';

interface MessageBubbleProps {
  message: ChatMessage;
  onPreviewImage?: (url: string) => void;
  onOpenProfile?: (username: string) => void;
}

export const MessageBubble: React.FC<MessageBubbleProps> = ({
  message,
  onPreviewImage,
  onOpenProfile,
}) => {
  const { isSelf, content, extra, status, timestamp, from_uid, from_name, from_role, from_avatar, type, seq } = message;
  const myAvatar = useAuthStore((s) => s.avatarUrl);

  const formatTime = (ts: number) => {
    const d = new Date(ts);
    return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
  };

  // 1. 系统官方公告 (SYSTEM_NOTICE) 居中全宽卡片渲染 (消除神秘用户气泡)
  if (type === MsgType.SYSTEM_NOTICE) {
    return (
      <div className="flex justify-center my-4 select-none px-4">
        <motion.div
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          className="max-w-lg w-full p-4 rounded-3xl bg-gradient-to-r from-[#F0EBE3] via-[#FAF8F5] to-[#F0EBE3] border border-amber-300/60 shadow-warm-sm flex items-start gap-3"
        >
          <div className="w-8 h-8 rounded-xl bg-gradient-to-tr from-amber-500 to-[#8B7355] text-white flex items-center justify-center shrink-0 shadow-sm mt-0.5">
            <Radio size={16} className="text-amber-100 animate-pulse" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-xs font-bold text-[#8B7355] flex items-center gap-1">
                <Sparkles size={11} />
                <span>全服系统官方公告</span>
              </span>
              <span className="text-[10px] text-[#A3927C] font-mono">
                {formatTime(timestamp)}
              </span>
            </div>
            <p className="text-xs text-[#2E2419] leading-relaxed mt-1 select-text">
              {content}
            </p>
          </div>
        </motion.div>
      </div>
    );
  }

  // 2. 常规单聊/群聊/大厅消息渲染
  let mediaMeta: { type?: 'image' | 'voice' | 'video'; url?: string; duration?: number; peaks?: number[] } | null = null;
  if (extra) {
    try {
      mediaMeta = JSON.parse(extra);
    } catch {}
  }

  const displayName = from_name || from_uid || '用户';

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.9, y: 12 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      transition={{ type: 'spring', stiffness: 400, damping: 28 }}
      className={`flex items-end gap-2.5 my-3 ${isSelf ? 'justify-end' : 'justify-start'}`}
    >
      {/* 对方头像 (点击打开个人主页，悬停展示 UID) */}
      {!isSelf && (
        <div
          title={`点击查看 @${displayName} 的个人主页与动态\nUID: ${from_uid}`}
          onClick={() => onOpenProfile?.(displayName)}
          className="w-8 h-8 rounded-xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] flex items-center justify-center font-bold text-xs text-[#FAF8F5] shadow-warm-sm shrink-0 cursor-pointer hover:scale-105 hover:shadow-walnut-glow transition-all select-none overflow-hidden"
        >
          {from_avatar ? (
            <img src={from_avatar} alt="头像" className="w-full h-full object-cover" />
          ) : (
            displayName.slice(0, 1).toUpperCase()
          )}
        </div>
      )}

      {/* 气泡主体 */}
      <div className={`flex flex-col max-w-md ${isSelf ? 'items-end' : 'items-start'}`}>
        {/* 发送者 Username 与身份牌 */}
        {!isSelf && (
          <div
            onClick={() => onOpenProfile?.(displayName)}
            className="flex items-center gap-1.5 mb-1 px-1 cursor-pointer select-text"
          >
            <span className="text-xs font-semibold text-[#8B7355] hover:underline">
              {displayName}
            </span>
            <RoleBadge role={from_role} username={displayName} size="sm" />
          </div>
        )}

        <div
          className={`relative px-4 py-2.5 rounded-2xl transition-all shadow-warm-sm ${
            isSelf
              ? 'bg-gradient-to-r from-[#8B7355] to-[#7A6348] text-[#FAF8F5] rounded-br-sm shadow-walnut-glow'
              : 'bg-[#F0EBE3] text-[#2E2419] border border-[#C9B99A]/40 rounded-bl-sm'
          }`}
        >
          {/* 1. 语音消息气泡 */}
          {mediaMeta && mediaMeta.type === 'voice' && mediaMeta.url ? (
            <AudioBubble
              audioUrl={mediaMeta.url}
              duration={mediaMeta.duration || 5}
              peaks={mediaMeta.peaks || [0.3, 0.6, 0.9, 0.4, 0.7, 0.5, 0.8, 0.3, 0.6, 0.4]}
              isSelf={isSelf}
            />
          ) : mediaMeta && mediaMeta.type === 'video' && mediaMeta.url ? (
            /* 2. 视频消息气泡 */
            <div className="space-y-1">
              <video
                src={mediaMeta.url}
                controls
                className="rounded-xl max-h-60 max-w-xs object-contain bg-black/20"
              />
              {content && content !== '[视频]' && (
                <p className="text-xs pt-1">{content}</p>
              )}
            </div>
          ) : mediaMeta && mediaMeta.type === 'image' && mediaMeta.url ? (
            /* 3. 图片消息气泡 */
            <div className="space-y-1">
              <img
                src={mediaMeta.url}
                alt="图片消息"
                onClick={() => onPreviewImage?.(mediaMeta!.url!)}
                className="rounded-xl max-h-60 object-cover cursor-pointer hover:opacity-95 transition-opacity"
              />
              {content && content !== '[图片]' && (
                <p className="text-xs pt-1">{content}</p>
              )}
            </div>
          ) : (
            /* 4. 纯文本消息 */
            <p className="text-sm leading-relaxed whitespace-pre-wrap break-words">
              {content}
            </p>
          )}

          {/* 底部时间与 ACK 状态 */}
          <div
            className={`flex items-center gap-1.5 mt-1 text-[10px] select-none ${
              isSelf ? 'justify-end text-[#FAF8F5]/70' : 'justify-start text-[#A3927C]'
            }`}
          >
            <span>{formatTime(timestamp)}</span>
            {isSelf && (
              <span className="flex items-center">
                {status === 'sending' && (
                  <Loader2 size={11} className="animate-spin text-white/70" />
                )}
                {status === 'success' && (
                  <span className="flex items-center gap-0.5 text-emerald-200">
                    <Check size={11} />
                    {seq > 0 && <span className="font-mono text-[9px]">#{seq}</span>}
                  </span>
                )}
                {status === 'error' && (
                  <span title="发送失败">
                    <AlertCircle size={11} className="text-red-300" />
                  </span>
                )}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* 己方头像 */}
      {isSelf && (
        <div className="w-8 h-8 rounded-xl bg-[#8B7355] flex items-center justify-center font-bold text-xs text-[#FAF8F5] shadow-warm-sm shrink-0 select-none overflow-hidden">
          {myAvatar ? (
            <img src={myAvatar} alt="我的头像" className="w-full h-full object-cover" />
          ) : (
            '我'
          )}
        </div>
      )}
    </motion.div>
  );
};
