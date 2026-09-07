import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  useFriendStore,
  useChatStore,
  useAuthStore,
  Conversation,
} from '../../store';
import { friendApi } from '../../api';
import {
  MessageSquare,
  Users,
  UserPlus,
  Bell,
  PlusCircle,
  Hash,
  ShieldCheck,
  Check,
  X,
  Search,
} from 'lucide-react';

interface SidebarProps {
  onOpenAddFriendModal: () => void;
  onOpenCreateGroupModal: () => void;
}

export const Sidebar: React.FC<SidebarProps> = ({
  onOpenAddFriendModal,
  onOpenCreateGroupModal,
}) => {
  const [tab, setTab] = useState<'hall' | 'friends' | 'groups' | 'requests'>('hall');
  const [searchTerm, setSearchTerm] = useState('');

  const { friends, pendingRequests, groups, fetchPendingRequests, fetchFriends } = useFriendStore();
  const { activeConversation, setActiveConversation } = useChatStore();
  const { userId } = useAuthStore();

  // 处理好友申请审批
  const handleRespondRequest = async (targetUserId: string, action: 'accept' | 'reject') => {
    try {
      await friendApi.respond(targetUserId, action);
      await fetchPendingRequests();
      if (action === 'accept') {
        await fetchFriends();
      }
    } catch (err) {
      console.error('审批申请失败:', err);
    }
  };

  return (
    <aside className="w-80 h-full flex flex-col glass-panel border-r border-[#C9B99A]/30 z-20 shrink-0 select-none">
      {/* 顶部四分类标签栏 */}
      <div className="p-3 border-b border-[#C9B99A]/20">
        <div className="flex p-1 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40">
          <button
            onClick={() => setTab('hall')}
            title="公共大厅"
            className={`relative flex-1 py-1.5 flex items-center justify-center rounded-xl text-xs font-semibold transition-all ${
              tab === 'hall' ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {tab === 'hall' && (
              <motion.div
                layoutId="sidebarTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10 flex items-center gap-1">
              <Hash size={14} />
              大厅
            </span>
          </button>

          <button
            onClick={() => setTab('friends')}
            title="好友列表"
            className={`relative flex-1 py-1.5 flex items-center justify-center rounded-xl text-xs font-semibold transition-all ${
              tab === 'friends' ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {tab === 'friends' && (
              <motion.div
                layoutId="sidebarTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10 flex items-center gap-1">
              <Users size={14} />
              好友
            </span>
          </button>

          <button
            onClick={() => setTab('groups')}
            title="我的群聊"
            className={`relative flex-1 py-1.5 flex items-center justify-center rounded-xl text-xs font-semibold transition-all ${
              tab === 'groups' ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {tab === 'groups' && (
              <motion.div
                layoutId="sidebarTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10 flex items-center gap-1">
              <MessageSquare size={14} />
              群聊
            </span>
          </button>

          <button
            onClick={() => setTab('requests')}
            title="待处理申请"
            className={`relative flex-1 py-1.5 flex items-center justify-center rounded-xl text-xs font-semibold transition-all ${
              tab === 'requests' ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {tab === 'requests' && (
              <motion.div
                layoutId="sidebarTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10 flex items-center gap-1">
              <Bell size={14} />
              {pendingRequests.length > 0 && (
                <span className="w-2 h-2 rounded-full bg-rose-500 animate-pulse" />
              )}
            </span>
          </button>
        </div>
      </div>

      {/* 搜索与新建快捷栏 */}
      <div className="px-3 py-2 flex items-center gap-2 border-b border-[#C9B99A]/20">
        <div className="relative flex-1 flex items-center">
          <Search size={14} className="absolute left-3 text-[#C9B99A]" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            placeholder="搜索联系人或群组..."
            className="w-full pl-8 pr-3 py-1.5 rounded-xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none"
          />
        </div>

        {tab === 'friends' && (
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={onOpenAddFriendModal}
            title="添加好友"
            className="p-1.5 rounded-xl bg-[#8B7355] text-[#FAF8F5] hover:bg-[#7A6348] shadow-warm-sm"
          >
            <UserPlus size={16} />
          </motion.button>
        )}

        {tab === 'groups' && (
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={onOpenCreateGroupModal}
            title="创建群聊"
            className="p-1.5 rounded-xl bg-[#8B7355] text-[#FAF8F5] hover:bg-[#7A6348] shadow-warm-sm"
          >
            <PlusCircle size={16} />
          </motion.button>
        )}
      </div>

      {/* 列表主体内容 */}
      <div className="flex-1 overflow-y-auto p-2 space-y-1">
        {/* 1. 公共大厅视图 */}
        {tab === 'hall' && (
          <motion.div
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            className="space-y-2"
          >
            <div
              onClick={() =>
                setActiveConversation({
                  id: 'hall',
                  type: 'hall',
                  name: '公共大厅',
                })
              }
              className={`p-3 rounded-2xl cursor-pointer transition-all flex items-center gap-3 ${
                activeConversation.id === 'hall'
                  ? 'bg-[#8B7355] text-[#FAF8F5] shadow-walnut-glow'
                  : 'glass-card hover:bg-[#F0EBE3] text-[#2E2419]'
              }`}
            >
              <div className="w-11 h-11 rounded-xl bg-[#C9B99A]/40 flex items-center justify-center font-bold text-lg">
                <Hash size={22} className={activeConversation.id === 'hall' ? 'text-[#FAF8F5]' : 'text-[#8B7355]'} />
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center justify-between">
                  <h3 className="font-semibold text-sm truncate">全服公共大厅</h3>
                  <span className={`text-[10px] px-1.5 py-0.5 rounded font-mono ${
                    activeConversation.id === 'hall' ? 'bg-white/20 text-white' : 'bg-[#C9B99A]/20 text-[#8B7355]'
                  }`}>
                    ALL
                  </span>
                </div>
                <p className={`text-xs truncate mt-0.5 ${
                  activeConversation.id === 'hall' ? 'text-white/80' : 'text-[#A3927C]'
                }`}>
                  点击进入全服即时公聊交流流
                </p>
              </div>
            </div>
          </motion.div>
        )}

        {/* 2. 好友列表视图 */}
        {tab === 'friends' && (
          <motion.div
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            className="space-y-1"
          >
            {friends.length === 0 ? (
              <div className="py-12 text-center text-xs text-[#A3927C]">
                暂无好友，点击右上角「+」添加
              </div>
            ) : (
              friends
                .filter((f) => f.username.toLowerCase().includes(searchTerm.toLowerCase()))
                .map((friend) => {
                  const isActive = activeConversation.id === friend.user_id;
                  return (
                    <div
                      key={friend.user_id}
                      onClick={() =>
                        setActiveConversation({
                          id: friend.user_id,
                          type: 'private',
                          name: friend.username,
                          avatar_url: friend.avatar_url,
                        })
                      }
                      className={`p-2.5 rounded-2xl cursor-pointer transition-all flex items-center gap-3 ${
                        isActive
                          ? 'bg-[#8B7355] text-[#FAF8F5] shadow-walnut-glow'
                          : 'glass-card hover:bg-[#F0EBE3] text-[#2E2419]'
                      }`}
                    >
                      <div className="relative">
                        <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] flex items-center justify-center font-bold text-sm text-[#FAF8F5]">
                          {friend.username.slice(0, 1).toUpperCase()}
                        </div>
                        <span className="absolute -bottom-0.5 -right-0.5 w-3 h-3 rounded-full bg-emerald-500 border-2 border-[#FAF8F5]" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between">
                          <h4 className="font-semibold text-sm truncate">{friend.username}</h4>
                        </div>
                        <p className={`text-xs truncate ${isActive ? 'text-white/80' : 'text-[#A3927C]'}`}>
                          {friend.signature || '暂无个性签名'}
                        </p>
                      </div>
                    </div>
                  );
                })
            )}
          </motion.div>
        )}

        {/* 3. 群聊列表视图 */}
        {tab === 'groups' && (
          <motion.div
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            className="space-y-1"
          >
            {groups.length === 0 ? (
              <div className="py-12 text-center text-xs text-[#A3927C]">
                未加入任何群聊，点击「+」新建群
              </div>
            ) : (
              groups
                .filter((g) => g.name.toLowerCase().includes(searchTerm.toLowerCase()))
                .map((grp) => {
                  const isActive = activeConversation.id === grp.id;
                  return (
                    <div
                      key={grp.id}
                      onClick={() =>
                        setActiveConversation({
                          id: grp.id,
                          type: 'group',
                          name: grp.name,
                          avatar_url: grp.avatar_url,
                        })
                      }
                      className={`p-2.5 rounded-2xl cursor-pointer transition-all flex items-center gap-3 ${
                        isActive
                          ? 'bg-[#8B7355] text-[#FAF8F5] shadow-walnut-glow'
                          : 'glass-card hover:bg-[#F0EBE3] text-[#2E2419]'
                      }`}
                    >
                      <div className="w-10 h-10 rounded-xl bg-[#8B7355]/20 flex items-center justify-center font-bold text-sm text-[#8B7355]">
                        <Users size={20} />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between">
                          <h4 className="font-semibold text-sm truncate">{grp.name}</h4>
                        </div>
                        <p className={`text-xs truncate ${isActive ? 'text-white/80' : 'text-[#A3927C]'}`}>
                          {grp.notice || '欢迎加入群交流'}
                        </p>
                      </div>
                    </div>
                  );
                })
            )}
          </motion.div>
        )}

        {/* 4. 待处理申请视图 */}
        {tab === 'requests' && (
          <motion.div
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            className="space-y-2"
          >
            {pendingRequests.length === 0 ? (
              <div className="py-12 text-center text-xs text-[#A3927C]">
                暂无待处理的好友申请
              </div>
            ) : (
              pendingRequests.map((req) => (
                <div
                  key={req.user_id}
                  className="p-3 rounded-2xl glass-card border border-[#C9B99A]/40 space-y-2"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-8 h-8 rounded-lg bg-[#C9B99A] text-white flex items-center justify-center font-bold text-xs">
                        {req.username.slice(0, 1).toUpperCase()}
                      </div>
                      <span className="font-semibold text-xs text-[#2E2419]">{req.username}</span>
                    </div>
                    <span className="text-[10px] text-[#A3927C]">申请加为好友</span>
                  </div>
                  {req.remark && (
                    <p className="text-xs text-[#8B7355] bg-[#F0EBE3] p-1.5 rounded-lg">
                      "{req.remark}"
                    </p>
                  )}
                  <div className="flex items-center gap-2 pt-1">
                    <button
                      onClick={() => handleRespondRequest(req.user_id, 'accept')}
                      className="flex-1 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center justify-center gap-1 hover:bg-[#7A6348] transition-colors"
                    >
                      <Check size={14} />
                      同意
                    </button>
                    <button
                      onClick={() => handleRespondRequest(req.user_id, 'reject')}
                      className="flex-1 py-1.5 rounded-xl bg-stone-200 text-stone-600 text-xs font-semibold flex items-center justify-center gap-1 hover:bg-stone-300 transition-colors"
                    >
                      <X size={14} />
                      拒绝
                    </button>
                  </div>
                </div>
              ))
            )}
          </motion.div>
        )}
      </div>
    </aside>
  );
};
