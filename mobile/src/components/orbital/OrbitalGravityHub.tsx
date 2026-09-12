import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Sparkles,
  Radio,
  Search,
  MessageCircle,
  Users,
  Compass,
  ArrowUpRight,
  Clock,
  Pin,
  Flame,
  Volume2,
  Image as ImageIcon,
  CheckCheck
} from 'lucide-react';
import { useChatStore, Conversation } from '../../store';
import { formatMediaUrl } from '../../utils/media';
import { soundEffects } from '../../audio/soundEffects';

interface OrbitalGravityHubProps {
  onEnterChat: (conv: Conversation) => void;
  onOpenProfile: (username: string) => void;
}

export const OrbitalGravityHub: React.FC<OrbitalGravityHubProps> = ({
  onEnterChat,
  onOpenProfile,
}) => {
  const { conversations, messages, activeBroadcastNotice } = useChatStore();
  const [filterType, setFilterType] = useState<'all' | 'private' | 'group' | 'unread'>('all');
  const [searchQuery, setSearchQuery] = useState('');

  // 获取大厅最新一条消息摘要
  const hallMessages = messages['hall'] || [];
  const latestHallMsg = hallMessages[hallMessages.length - 1];

  // 过滤会话列表（剔除大厅本身，大厅作为顶部太阳核心展示）
  const activeConversations = conversations.filter((c) => c.id !== 'hall');

  const filteredConversations = activeConversations.filter((c) => {
    // 搜索过滤
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      const matchName = c.name.toLowerCase().includes(q);
      const matchMsg = c.lastMessage?.toLowerCase().includes(q);
      if (!matchName && !matchMsg) return false;
    }

    // 分类过滤
    if (filterType === 'private') return c.type === 'private';
    if (filterType === 'group') return c.type === 'group';
    if (filterType === 'unread') return (c.unreadCount || 0) > 0;
    return true;
  });

  const handleSelect = (conv: Conversation) => {
    soundEffects.playHapticTick();
    onEnterChat(conv);
  };

  const hallConv: Conversation = {
    id: 'hall',
    type: 'hall',
    name: '全服公共大厅',
  };

  return (
    <div className="w-full h-full flex flex-col overflow-y-auto px-4 pt-6 pb-28 select-none">
      {/* 1. 顶部全服广播条 (若有) */}
      <AnimatePresence>
        {activeBroadcastNotice && (
          <motion.div
            initial={{ opacity: 0, y: -12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -12 }}
            className="mb-4 p-3 rounded-2xl bg-gradient-to-r from-amber-500/15 via-[#C9B99A]/20 to-amber-500/15 border border-amber-400/40 flex items-center gap-2.5 text-xs text-[#8B7355] shadow-warm-sm"
          >
            <Radio size={15} className="text-amber-600 animate-pulse shrink-0" />
            <span className="font-semibold truncate flex-1">
              {activeBroadcastNotice.content}
            </span>
          </motion.div>
        )}
      </AnimatePresence>

      {/* 2. 空间引力标题与搜索栏 */}
      <div className="flex items-center justify-between mb-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-black tracking-tight text-[#2E2419]">
              引力场
            </h1>
            <span className="px-2 py-0.5 text-[10px] font-bold rounded-full bg-[#8B7355]/15 text-[#8B7355] tracking-wider uppercase">
              Orbital Neo
            </span>
          </div>
          <p className="text-xs text-[#A3927C] mt-0.5 font-medium">
            实时流体引力枢纽 · 单手漫游
          </p>
        </div>

        {/* 动态脉冲徽章 */}
        <div className="w-9 h-9 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 flex items-center justify-center text-[#8B7355] shadow-warm-sm">
          <Compass size={18} className="animate-spin-slow" />
        </div>
      </div>

      {/* 3. 搜索与微胶囊筛选器 */}
      <div className="space-y-3 mb-5">
        <div className="relative">
          <Search size={15} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[#A3927C]" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="在引力场中搜寻好友、群组或动态..."
            className="w-full pl-9 pr-4 py-2.5 rounded-2xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none shadow-warm-xs"
          />
          {searchQuery && (
            <button
              onClick={() => setSearchQuery('')}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-[#A3927C] hover:text-[#2E2419]"
            >
              清空
            </button>
          )}
        </div>

        {/* 流体胶囊过滤条 */}
        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar py-0.5">
          {[
            { id: 'all', label: '全部引力' },
            { id: 'private', label: '私聊双星' },
            { id: 'group', label: '星团群聊' },
            { id: 'unread', label: '未读潮涌' },
          ].map((item) => {
            const isSelected = filterType === item.id;
            return (
              <button
                key={item.id}
                onClick={() => {
                  soundEffects.playHapticTick();
                  setFilterType(item.id as any);
                }}
                className={`px-3 py-1.5 rounded-xl text-xs font-semibold whitespace-nowrap transition-all ${
                  isSelected
                    ? 'bg-[#8B7355] text-white shadow-walnut-glow'
                    : 'bg-[#F0EBE3]/80 text-[#8B7355] hover:bg-[#E8E2D8]'
                }`}
              >
                {item.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* 4. 太阳核心 ——【全服公共大厅引力核心卡片】 */}
      <motion.div
        whileTap={{ scale: 0.98 }}
        onClick={() => handleSelect(hallConv)}
        className="relative mb-6 p-5 rounded-3xl bg-gradient-to-br from-[#8B7355] via-[#7A6348] to-[#5C4834] text-[#FAF8F5] shadow-walnut-glow cursor-pointer overflow-hidden border border-[#C9B99A]/40 group"
      >
        {/* 背景光晕装饰 */}
        <div className="absolute -right-8 -top-8 w-36 h-36 rounded-full bg-amber-400/20 blur-2xl pointer-events-none" />
        <div className="absolute -left-6 -bottom-6 w-32 h-32 rounded-full bg-[#C9B99A]/20 blur-xl pointer-events-none" />

        <div className="relative z-10">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2.5">
              <div className="w-10 h-10 rounded-2xl bg-white/15 backdrop-blur-md border border-white/20 flex items-center justify-center shadow-inner">
                <Flame size={20} className="text-amber-300 animate-pulse" />
              </div>
              <div>
                <div className="flex items-center gap-1.5">
                  <h3 className="font-black text-sm tracking-wide text-white">
                    全服公共大厅
                  </h3>
                  <span className="w-2 h-2 rounded-full bg-emerald-400 shadow-[0_0_8px_#34d399] animate-pulse" />
                </div>
                <p className="text-[10px] text-white/70 font-mono">
                  THE GRAND LOBBY PULSAR
                </p>
              </div>
            </div>

            <div className="flex items-center gap-1 text-[11px] font-semibold text-amber-200 bg-white/10 px-2.5 py-1 rounded-full backdrop-blur-sm group-hover:translate-x-0.5 transition-transform">
              <span>立即跃迁</span>
              <ArrowUpRight size={13} />
            </div>
          </div>

          {/* 大厅最新一条弹幕 */}
          <div className="p-3 rounded-2xl bg-black/20 backdrop-blur-sm border border-white/10 flex items-center gap-2">
            <span className="text-[10px] font-bold text-amber-300 px-1.5 py-0.5 rounded bg-amber-500/20 shrink-0">
              最新共鸣
            </span>
            <p className="text-xs text-white/90 truncate flex-1 font-light">
              {latestHallMsg
                ? `${latestHallMsg.from_name || '探索者'}: ${latestHallMsg.content}`
                : '静谧的星海中... 点击率先发布全服第一声共鸣'}
            </p>
          </div>
        </div>
      </motion.div>

      {/* 5. 活跃引力会话卡片网格/流 (Orbital Cards) */}
      <div className="space-y-3">
        <div className="flex items-center justify-between px-1">
          <span className="text-xs font-bold text-[#8B7355] flex items-center gap-1.5">
            <Sparkles size={14} />
            <span>共振活跃列表 ({filteredConversations.length})</span>
          </span>
          <span className="text-[10px] text-[#A3927C]">轻触即连 · 自由漫游</span>
        </div>

        {filteredConversations.length === 0 ? (
          <div className="py-14 text-center space-y-2 rounded-3xl bg-[#F0EBE3]/40 border border-dashed border-[#C9B99A]/50 p-6">
            <div className="w-12 h-12 rounded-2xl bg-[#F0EBE3] text-[#8B7355] flex items-center justify-center mx-auto shadow-warm-xs">
              <Compass size={24} />
            </div>
            <p className="text-xs font-semibold text-[#2E2419]">当前无匹配会话</p>
            <p className="text-[11px] text-[#A3927C] max-w-xs mx-auto">
              可以通过右下角灵动胶囊的「+」号发起私聊或探索新朋友
            </p>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2.5">
            {filteredConversations.map((conv) => {
              const isGroup = conv.type === 'group';
              const unread = conv.unreadCount || 0;

              return (
                <motion.div
                  key={conv.id}
                  whileTap={{ scale: 0.98 }}
                  onClick={() => handleSelect(conv)}
                  className="p-3.5 rounded-3xl bg-[#FAF8F5] border border-[#C9B99A]/40 shadow-warm-sm hover:shadow-warm-md hover:border-[#8B7355]/40 transition-all flex items-center gap-3.5 cursor-pointer relative overflow-hidden group"
                >
                  {/* 头像区域 */}
                  <div className="relative shrink-0">
                    <div
                      onClick={(e) => {
                        if (!isGroup) {
                          e.stopPropagation();
                          onOpenProfile(conv.name);
                        }
                      }}
                      className={`w-12 h-12 rounded-2xl flex items-center justify-center font-bold text-sm shadow-warm-sm overflow-hidden ${
                        isGroup
                          ? 'bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white'
                          : 'bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] text-white'
                      }`}
                    >
                      {conv.avatar_url ? (
                        <img
                          src={formatMediaUrl(conv.avatar_url)}
                          alt={conv.name}
                          className="w-full h-full object-cover"
                        />
                      ) : isGroup ? (
                        <Users size={20} />
                      ) : (
                        conv.name.slice(0, 2).toUpperCase()
                      )}
                    </div>

                    {/* 未读呼吸红点 */}
                    {unread > 0 && (
                      <span className="absolute -top-1 -right-1 min-w-[18px] h-[18px] px-1 bg-gradient-to-r from-rose-500 to-amber-600 text-white font-mono text-[10px] font-black rounded-full flex items-center justify-center shadow-md animate-bounce">
                        {unread > 99 ? '99+' : unread}
                      </span>
                    )}
                  </div>

                  {/* 会话文字详情 */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between mb-1">
                      <div className="flex items-center gap-1.5 truncate">
                        <h4 className="font-bold text-xs text-[#2E2419] truncate group-hover:text-[#8B7355] transition-colors">
                          {conv.name}
                        </h4>
                        {isGroup && (
                          <span className="text-[9px] px-1.5 py-0.2 rounded-md bg-[#8B7355]/10 text-[#8B7355] font-semibold shrink-0">
                            群
                          </span>
                        )}
                      </div>
                      <span className="text-[10px] text-[#A3927C] font-mono shrink-0 flex items-center gap-0.5">
                        <Clock size={10} />
                        <span>活跃</span>
                      </span>
                    </div>

                    <p className="text-xs text-[#8B7355]/80 truncate font-normal">
                      {conv.lastMessage || (isGroup ? '暂无群消息' : '暂无对话记录')}
                    </p>
                  </div>
                </motion.div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
