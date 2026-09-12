import React, { useEffect, useRef } from 'react';
import { ChatMessage, Conversation } from '../../store';
import { MessageBubble } from './MessageBubble';
import { Sparkles, History } from 'lucide-react';

interface MessageListProps {
  messages: ChatMessage[];
  conversation: Conversation;
  onLoadMoreHistory?: () => void;
  onPreviewImage?: (url: string) => void;
  onOpenProfile?: (username: string) => void;
}

export const MessageList: React.FC<MessageListProps> = ({
  messages,
  conversation,
  onLoadMoreHistory,
  onPreviewImage,
  onOpenProfile,
}) => {
  const bottomRef = useRef<HTMLDivElement | null>(null);

  // 当有新消息时平滑滚动到底部
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  return (
    <div className="flex-1 overflow-y-auto px-6 py-4 space-y-2">
      {/* 顶部加载更多历史消息按钮 */}
      {messages.length > 0 && (
        <div className="flex justify-center my-2">
          <button
            onClick={onLoadMoreHistory}
            className="flex items-center gap-1.5 px-3 py-1 rounded-full text-[11px] font-medium text-[#8B7355] bg-[#F0EBE3] hover:bg-[#EAE4DC] border border-[#C9B99A]/30 transition-all shadow-warm-sm"
          >
            <History size={13} />
            查看历史消息
          </button>
        </div>
      )}

      {/* 空状态引导 */}
      {messages.length === 0 ? (
        <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-3 opacity-80">
          <div className="w-16 h-16 rounded-3xl bg-[#F0EBE3] border border-[#C9B99A]/40 flex items-center justify-center text-[#8B7355] shadow-warm-md">
            <Sparkles size={28} />
          </div>
          <div>
            <h3 className="font-bold text-[#2E2419] text-base">
              {conversation.name}
            </h3>
            <p className="text-xs text-[#A3927C] mt-1 max-w-xs">
              暂无历史消息。在这片如沐暖阳的温暖空间里，开启你们的第一句问候吧。
            </p>
          </div>
        </div>
      ) : (
        messages.map((msg) => (
          <MessageBubble
            key={msg.stanza_id || msg.id}
            message={msg}
            onPreviewImage={onPreviewImage}
            onOpenProfile={onOpenProfile}
          />
        ))
      )}

      <div ref={bottomRef} className="h-2" />
    </div>
  );
};
