import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Users,
  UserPlus,
  Search,
  MessageSquare,
  Check,
  X,
  Users2,
  Sparkles,
  Shield,
  Loader2,
  UserMinus,
  ChevronRight
} from 'lucide-react';
import { useFriendStore, useChatStore, Conversation } from '../../store';
import { friendApi, FriendItem } from '../../api';
import { formatMediaUrl } from '../../utils/media';
import { RoleBadge } from '../common/RoleBadge';
import { soundEffects } from '../../audio/soundEffects';

interface ContactsGalaxyViewProps {
  onEnterChat: (conv: Conversation) => void;
  onOpenProfile: (username: string) => void;
  onOpenAddFriend: () => void;
  onOpenCreateGroup: () => void;
}

export const ContactsGalaxyView: React.FC<ContactsGalaxyViewProps> = ({
  onEnterChat,
  onOpenProfile,
  onOpenAddFriend,
  onOpenCreateGroup,
}) => {
  const {
    friends,
    groups,
    pendingRequests,
    fetchFriends,
    fetchGroups,
    fetchPendingRequests,
    removeFriend
  } = useFriendStore();

  const [activeSection, setActiveSection] = useState<'all' | 'friends' | 'groups'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [respondingId, setRespondingId] = useState<string | null>(null);

  useEffect(() => {
    fetchFriends();
    fetchGroups();
    fetchPendingRequests();
  }, [fetchFriends, fetchGroups, fetchPendingRequests]);

  // 处理好友申请
  const handleRespondRequest = async (targetUserId: string, accept: boolean) => {
    setRespondingId(targetUserId);
    soundEffects.playHapticTick();
    try {
      await friendApi.respond(targetUserId, accept ? 'accept' : 'reject');
      await fetchPendingRequests();
      await fetchFriends();
    } catch (err) {
      console.error('处理好友申请失败:', err);
    } finally {
      setRespondingId(null);
    }
  };

  const handleStartFriendChat = (friend: FriendItem) => {
    soundEffects.playHapticTick();
    const conv: Conversation = {
      id: friend.user_id,
      type: 'private',
      name: friend.remark || friend.username,
      avatar_url: friend.avatar_url,
    };
    onEnterChat(conv);
  };

  const handleStartGroupChat = (group: any) => {
    soundEffects.playHapticTick();
    const conv: Conversation = {
      id: group.id,
      type: 'group',
      name: group.name,
      avatar_url: group.avatar_url,
    };
    onEnterChat(conv);
  };

  // 过滤好友
  const filteredFriends = friends.filter((f) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase();
    return (
      f.username.toLowerCase().includes(q) ||
      (f.remark && f.remark.toLowerCase().includes(q))
    );
  });

  // 过滤群组
  const filteredGroups = groups.filter((g) => {
    if (!searchQuery.trim()) return true;
    return g.name.toLowerCase().includes(searchQuery.toLowerCase());
  });

  return (
    <div className="w-full h-full flex flex-col overflow-y-auto px-4 pt-6 pb-28 select-none">
      {/* 1. 标题与统计 */}
      <div className="flex items-center justify-between mb-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-black tracking-tight text-[#2E2419]">
              星系通讯
            </h1>
            <span className="px-2 py-0.5 text-[10px] font-bold rounded-full bg-[#8B7355]/15 text-[#8B7355] tracking-wider uppercase">
              GALAXY
            </span>
          </div>
          <p className="text-xs text-[#A3927C] mt-0.5 font-medium">
            好友与群聊共鸣群 · 人脉星图
          </p>
        </div>

        <div className="flex items-center gap-1.5">
          <button
            onClick={onOpenAddFriend}
            className="px-3 py-1.5 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-xs font-semibold text-[#8B7355] flex items-center gap-1 shadow-warm-xs active:scale-95 transition-transform"
          >
            <UserPlus size={14} />
            <span>加好友</span>
          </button>
        </div>
      </div>

      {/* 2. 搜索框 */}
      <div className="relative mb-4">
        <Search size={15} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[#A3927C]" />
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="搜索好友昵称、用户名或群聊..."
          className="w-full pl-9 pr-4 py-2.5 rounded-2xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none shadow-warm-xs"
        />
      </div>

      {/* 3. 待处理好友申请高亮专区 (若有待办) */}
      <AnimatePresence>
        {pendingRequests.length > 0 && (
          <motion.div
            initial={{ opacity: 0, scale: 0.95 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.95 }}
            className="mb-5 p-4 rounded-3xl bg-gradient-to-r from-amber-500/10 via-[#FAF8F5] to-amber-500/10 border border-amber-300 shadow-warm-sm space-y-3"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold text-amber-900 flex items-center gap-1.5">
                <Sparkles size={14} className="text-amber-600" />
                <span>待处理的好友引力申请 ({pendingRequests.length})</span>
              </span>
            </div>

            <div className="space-y-2">
              {pendingRequests.map((req) => (
                <div
                  key={req.user_id}
                  className="p-3 rounded-2xl bg-white/90 border border-amber-200/70 shadow-warm-xs flex items-center justify-between gap-3"
                >
                  <div
                    onClick={() => onOpenProfile(req.username)}
                    className="flex items-center gap-2.5 cursor-pointer truncate flex-1"
                  >
                    <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-amber-500 to-[#8B7355] text-white flex items-center justify-center font-bold text-xs shrink-0 shadow-sm overflow-hidden">
                      {req.username.slice(0, 1).toUpperCase()}
                    </div>
                    <div className="min-w-0">
                      <div className="text-xs font-bold text-[#2E2419] truncate">
                        @{req.username}
                      </div>
                      <div className="text-[10px] text-[#A3927C] truncate">
                        {req.remark || '请求添加你为好友'}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      disabled={respondingId === req.user_id}
                      onClick={() => handleRespondRequest(req.user_id, true)}
                      className="p-2 rounded-xl bg-emerald-600 text-white hover:bg-emerald-700 shadow-sm active:scale-95 transition-transform"
                      title="同意申请"
                    >
                      {respondingId === req.user_id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : (
                        <Check size={14} />
                      )}
                    </button>
                    <button
                      disabled={respondingId === req.user_id}
                      onClick={() => handleRespondRequest(req.user_id, false)}
                      className="p-2 rounded-xl bg-stone-200 text-stone-600 hover:bg-stone-300 shadow-sm active:scale-95 transition-transform"
                      title="忽略"
                    >
                      <X size={14} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* 4. 分类切片条 */}
      <div className="flex items-center gap-2 mb-4">
        {[
          { id: 'all', label: `全部星图 (${filteredFriends.length + filteredGroups.length})` },
          { id: 'friends', label: `好友 (${filteredFriends.length})` },
          { id: 'groups', label: `群组 (${filteredGroups.length})` },
        ].map((sec) => (
          <button
            key={sec.id}
            onClick={() => {
              soundEffects.playHapticTick();
              setActiveSection(sec.id as any);
            }}
            className={`px-3 py-1.5 rounded-xl text-xs font-semibold transition-all ${
              activeSection === sec.id
                ? 'bg-[#8B7355] text-white shadow-walnut-glow'
                : 'bg-[#F0EBE3]/80 text-[#8B7355] hover:bg-[#EAE4DC]'
            }`}
          >
            {sec.label}
          </button>
        ))}
      </div>

      {/* 5. 星团群聊分组 */}
      {(activeSection === 'all' || activeSection === 'groups') && filteredGroups.length > 0 && (
        <div className="mb-6 space-y-2">
          <div className="flex items-center justify-between px-1">
            <span className="text-xs font-bold text-[#8B7355] flex items-center gap-1.5">
              <Users2 size={14} />
              <span>加入的群聊 ({filteredGroups.length})</span>
            </span>
            <button
              onClick={onOpenCreateGroup}
              className="text-[11px] text-[#8B7355] hover:underline font-semibold"
            >
              + 建群
            </button>
          </div>

          <div className="grid grid-cols-1 gap-2">
            {filteredGroups.map((grp) => (
              <motion.div
                key={grp.id}
                whileTap={{ scale: 0.98 }}
                onClick={() => handleStartGroupChat(grp)}
                className="p-3 rounded-2xl bg-[#FAF8F5] border border-[#C9B99A]/40 shadow-warm-xs hover:border-[#8B7355]/40 transition-all flex items-center justify-between cursor-pointer group"
              >
                <div className="flex items-center gap-3 truncate">
                  <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white flex items-center justify-center font-bold text-xs shadow-warm-sm shrink-0">
                    <Users size={18} />
                  </div>
                  <div className="truncate">
                    <h4 className="text-xs font-bold text-[#2E2419] group-hover:text-[#8B7355] transition-colors truncate">
                      {grp.name}
                    </h4>
                    <p className="text-[10px] text-[#A3927C]">
                      群ID: {grp.id.slice(0, 8)}...
                    </p>
                  </div>
                </div>

                <div className="p-2 rounded-xl bg-[#F0EBE3] text-[#8B7355] group-hover:bg-[#8B7355] group-hover:text-white transition-colors">
                  <MessageSquare size={14} />
                </div>
              </motion.div>
            ))}
          </div>
        </div>
      )}

      {/* 6. 好友星系列表 */}
      {(activeSection === 'all' || activeSection === 'friends') && (
        <div className="space-y-2">
          <div className="flex items-center justify-between px-1">
            <span className="text-xs font-bold text-[#8B7355] flex items-center gap-1.5">
              <Users size={14} />
              <span>好友列表 ({filteredFriends.length})</span>
            </span>
          </div>

          {filteredFriends.length === 0 ? (
            <div className="py-12 text-center text-xs text-[#A3927C] rounded-2xl bg-[#F0EBE3]/40 border border-dashed border-[#C9B99A]/40 p-4">
              未找到相关好友，点击右上角加好友扩大你的引力圈
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-2">
              {filteredFriends.map((f) => (
                <motion.div
                  key={f.user_id}
                  whileTap={{ scale: 0.98 }}
                  onClick={() => handleStartFriendChat(f)}
                  className="p-3 rounded-2xl bg-[#FAF8F5] border border-[#C9B99A]/40 shadow-warm-xs hover:border-[#8B7355]/40 transition-all flex items-center justify-between cursor-pointer group"
                >
                  <div className="flex items-center gap-3 truncate flex-1 mr-2">
                    <div
                      onClick={(e) => {
                        e.stopPropagation();
                        onOpenProfile(f.username);
                      }}
                      className="w-11 h-11 rounded-2xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] text-white flex items-center justify-center font-bold text-xs shadow-warm-sm shrink-0 overflow-hidden"
                    >
                      {f.avatar_url ? (
                        <img
                          src={formatMediaUrl(f.avatar_url)}
                          alt={f.username}
                          className="w-full h-full object-cover"
                        />
                      ) : (
                        f.username.slice(0, 1).toUpperCase()
                      )}
                    </div>

                    <div className="truncate">
                      <div className="flex items-center gap-1.5">
                        <h4 className="text-xs font-bold text-[#2E2419] group-hover:text-[#8B7355] transition-colors truncate">
                          {f.remark || f.username}
                        </h4>
                        <RoleBadge username={f.username} size="sm" />
                      </div>
                      <p className="text-[10px] text-[#A3927C] font-mono mt-0.5">
                        @{f.username}
                      </p>
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        onOpenProfile(f.username);
                      }}
                      className="p-2 rounded-xl bg-[#F0EBE3] text-[#8B7355] hover:bg-[#EAE4DC] transition-colors text-[11px] font-semibold"
                      title="查看资料"
                    >
                      主页
                    </button>
                    <div className="p-2 rounded-xl bg-[#8B7355] text-white shadow-warm-xs">
                      <MessageSquare size={14} />
                    </div>
                  </div>
                </motion.div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};
