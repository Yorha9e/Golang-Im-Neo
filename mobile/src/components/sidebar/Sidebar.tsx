import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  useFriendStore,
  useChatStore,
  useAuthStore,
} from '../../store';
import { friendApi, FriendItem } from '../../api';
import { formatMediaUrl } from '../../utils/media';
import {
  MessageSquare,
  Users,
  UserPlus,
  Bell,
  PlusCircle,
  Hash,
  Check,
  X,
  Search,
  RotateCw,
  UserMinus,
  User,
  AlertTriangle,
  Loader2,
} from 'lucide-react';

interface SidebarProps {
  onOpenAddFriendModal: () => void;
  onOpenCreateGroupModal: () => void;
  onOpenProfile?: (username: string) => void;
  onSelectConversation?: () => void;
}

export const Sidebar: React.FC<SidebarProps> = ({
  onOpenAddFriendModal,
  onOpenCreateGroupModal,
  onOpenProfile,
  onSelectConversation,
}) => {
  const [tab, setTab] = useState<'hall' | 'friends' | 'groups' | 'requests'>('hall');
  const [searchTerm, setSearchTerm] = useState('');
  const [isManualRefreshing, setIsManualRefreshing] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<FriendItem | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [respondingId, setRespondingId] = useState<string | null>(null);

  const {
    friends,
    pendingRequests,
    groups,
    isLoading,
    isRefreshing,
    fetchFriends,
    fetchPendingRequests,
    fetchGroups,
    fetchAll,
    removeFriend,
  } = useFriendStore();

  const { activeConversation, setActiveConversation } = useChatStore();
  const { userId } = useAuthStore();

  // 1. 组件首次挂载时拉取全量数据
  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  // 2. 切换 Tab 时自动定向刷新当前 Tab 数据，避免数据陈旧
  useEffect(() => {
    if (tab === 'friends') {
      fetchFriends();
    } else if (tab === 'requests') {
      fetchPendingRequests();
    } else if (tab === 'groups') {
      fetchGroups();
    }
  }, [tab, fetchFriends, fetchPendingRequests, fetchGroups]);

  // 手动一键刷新当前模块数据
  const handleManualRefresh = async () => {
    setIsManualRefreshing(true);
    try {
      if (tab === 'friends') {
        await fetchFriends();
      } else if (tab === 'requests') {
        await fetchPendingRequests();
      } else if (tab === 'groups') {
        await fetchGroups();
      } else {
        await fetchAll();
      }
    } finally {
      setTimeout(() => {
        setIsManualRefreshing(false);
      }, 500);
    }
  };

  // 处理好友申请审批
  const handleRespondRequest = async (targetUserId: string, action: 'accept' | 'reject') => {
    setRespondingId(targetUserId);
    try {
      await friendApi.respond(targetUserId, action);
      await fetchPendingRequests();
      if (action === 'accept') {
        await fetchFriends();
      }
    } catch (err) {
      console.error('审批申请失败:', err);
    } finally {
      setRespondingId(null);
    }
  };

  // 执行解除好友操作
  const handleConfirmDeleteFriend = async () => {
    if (!deleteTarget) return;
    setIsDeleting(true);
    try {
      const ok = await removeFriend(deleteTarget.user_id);
      if (ok) {
        // 如果当前会话正是该好友，自动切换回公共大厅
        if (activeConversation.id === deleteTarget.user_id) {
          setActiveConversation({
            id: 'hall',
            type: 'hall',
            name: '公共大厅',
          });
        }
      }
      setDeleteTarget(null);
    } catch (err) {
      console.error('解除好友失败:', err);
    } finally {
      setIsDeleting(false);
    }
  };

  const refreshing = isRefreshing || isManualRefreshing;

  return (
    <aside className="w-full md:w-80 h-full flex flex-col glass-panel border-r border-[#C9B99A]/30 z-20 shrink-0 select-none relative pb-[env(safe-area-inset-bottom,0px)]">
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
              {friends.length > 0 && (
                <span className={`text-[10px] px-1 py-0.2 rounded-full ${
                  tab === 'friends' ? 'bg-white/25 text-white' : 'bg-[#8B7355]/20 text-[#8B7355]'
                }`}>
                  {friends.length}
                </span>
              )}
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
              申请
              {pendingRequests.length > 0 && (
                <span className="w-2 h-2 rounded-full bg-rose-500 animate-pulse" />
              )}
            </span>
          </button>
        </div>
      </div>

      {/* 搜索、刷新与新建快捷栏 */}
      <div className="px-3 py-2 flex items-center gap-1.5 border-b border-[#C9B99A]/20">
        <div className="relative flex-1 flex items-center">
          <Search size={14} className="absolute left-3 text-[#C9B99A]" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            placeholder="搜索好友或群聊..."
            className="w-full pl-8 pr-3 py-1.5 rounded-xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none"
          />
        </div>

        {/* 手动一键刷新按钮 */}
        <motion.button
          whileTap={{ scale: 0.88 }}
          onClick={handleManualRefresh}
          disabled={refreshing}
          title="刷新当前数据列表"
          className="p-1.5 rounded-xl border border-[#C9B99A]/40 bg-[#F0EBE3] text-[#8B7355] hover:bg-[#EAE4DC] transition-colors disabled:opacity-50 shrink-0"
        >
          <RotateCw size={15} className={refreshing ? 'animate-spin text-[#8B7355]' : ''} />
        </motion.button>

        {tab === 'friends' && (
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={onOpenAddFriendModal}
            title="添加好友"
            className="p-1.5 rounded-xl bg-[#8B7355] text-[#FAF8F5] hover:bg-[#7A6348] shadow-warm-sm shrink-0"
          >
            <UserPlus size={15} />
          </motion.button>
        )}

        {tab === 'groups' && (
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={onOpenCreateGroupModal}
            title="创建群聊"
            className="p-1.5 rounded-xl bg-[#8B7355] text-[#FAF8F5] hover:bg-[#7A6348] shadow-warm-sm shrink-0"
          >
            <PlusCircle size={15} />
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
              onClick={() => {
                setActiveConversation({
                  id: 'hall',
                  type: 'hall',
                  name: '公共大厅',
                });
                onSelectConversation?.();
              }}
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
                  全员公共交流频道 · 支持漫游
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
            {/* 加载中骨架屏 */}
            {refreshing && friends.length === 0 ? (
              <div className="space-y-2 p-1">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="p-3 rounded-2xl glass-card border border-[#C9B99A]/30 flex items-center gap-3 animate-pulse">
                    <div className="w-10 h-10 rounded-xl bg-[#C9B99A]/40" />
                    <div className="flex-1 space-y-1.5">
                      <div className="h-3.5 bg-[#C9B99A]/40 rounded w-24" />
                      <div className="h-2.5 bg-[#C9B99A]/20 rounded w-36" />
                    </div>
                  </div>
                ))}
              </div>
            ) : friends.length === 0 ? (
              <div className="py-12 px-4 text-center space-y-3">
                <div className="w-12 h-12 mx-auto rounded-2xl bg-[#F0EBE3] flex items-center justify-center text-[#8B7355]">
                  <Users size={24} />
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-semibold text-[#2E2419]">暂无好友记录</p>
                  <p className="text-[11px] text-[#A3927C]">可能尚未添加好友，或网络初次同步中</p>
                </div>
                <div className="flex items-center justify-center gap-2 pt-1">
                  <button
                    onClick={handleManualRefresh}
                    className="px-3 py-1.5 rounded-xl border border-[#C9B99A]/50 text-xs text-[#8B7355] hover:bg-[#F0EBE3] flex items-center gap-1"
                  >
                    <RotateCw size={12} className={refreshing ? 'animate-spin' : ''} />
                    <span>刷新列表</span>
                  </button>
                  <button
                    onClick={onOpenAddFriendModal}
                    className="px-3 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1 shadow-warm-sm hover:bg-[#7A6348]"
                  >
                    <UserPlus size={12} />
                    <span>添加好友</span>
                  </button>
                </div>
              </div>
            ) : (
              friends
                .filter((f) =>
                  (f.username || '').toLowerCase().includes(searchTerm.toLowerCase()) ||
                  (f.remark || '').toLowerCase().includes(searchTerm.toLowerCase())
                )
                .map((friend) => {
                  const isActive = activeConversation.id === friend.user_id;
                  const displayName = friend.remark || friend.username;
                  const hasRemark = Boolean(friend.remark && friend.remark !== friend.username);

                  return (
                    <div
                      key={friend.user_id}
                      onClick={() => {
                        setActiveConversation({
                          id: friend.user_id,
                          type: 'private',
                          name: friend.username,
                          avatar_url: friend.avatar_url,
                        });
                        onSelectConversation?.();
                      }}
                      className={`group relative p-2.5 rounded-2xl cursor-pointer transition-all flex items-center gap-3 ${
                        isActive
                          ? 'bg-[#8B7355] text-[#FAF8F5] shadow-walnut-glow'
                          : 'glass-card hover:bg-[#F0EBE3] text-[#2E2419]'
                      }`}
                    >
                      {/* 头像 */}
                      <div className="relative shrink-0">
                        <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] flex items-center justify-center font-bold text-sm text-[#FAF8F5] overflow-hidden shadow-warm-sm">
                          {friend.avatar_url ? (
                            <img src={formatMediaUrl(friend.avatar_url)} alt="头像" className="w-full h-full object-cover" />
                          ) : (
                            friend.username.slice(0, 1).toUpperCase()
                          )}
                        </div>
                        <span className="absolute -bottom-0.5 -right-0.5 w-3 h-3 rounded-full bg-emerald-500 border-2 border-[#FAF8F5]" />
                      </div>

                      {/* 昵称与签名 */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-1.5">
                          <h4 className="font-semibold text-sm truncate">{displayName}</h4>
                          {hasRemark && (
                            <span className={`text-[10px] font-mono truncate ${
                              isActive ? 'text-white/70' : 'text-[#8B7355]/70'
                            }`}>
                              @{friend.username}
                            </span>
                          )}
                        </div>
                        <p className={`text-xs truncate mt-0.5 ${isActive ? 'text-white/80' : 'text-[#A3927C]'}`}>
                          {friend.signature || '「细腻暖沙，静候知音」'}
                        </p>
                      </div>

                      {/* 悬浮快捷操作按钮组 */}
                      <div
                        onClick={(e) => e.stopPropagation()}
                        className={`items-center gap-1 ${
                          isActive ? 'flex' : 'hidden group-hover:flex'
                        }`}
                      >
                        {/* 查看主页 */}
                        <button
                          onClick={() => onOpenProfile?.(friend.username)}
                          title="查看该用户主页与动态广场"
                          className={`p-1.5 rounded-lg transition-colors ${
                            isActive ? 'hover:bg-white/20 text-white' : 'hover:bg-[#C9B99A]/30 text-[#8B7355]'
                          }`}
                        >
                          <User size={13} />
                        </button>

                        {/* 解除好友 */}
                        <button
                          onClick={() => setDeleteTarget(friend)}
                          title="解除好友关系"
                          className={`p-1.5 rounded-lg transition-colors ${
                            isActive ? 'hover:bg-red-500/30 text-rose-200' : 'hover:bg-rose-100 text-rose-600'
                          }`}
                        >
                          <UserMinus size={13} />
                        </button>
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
            {refreshing && groups.length === 0 ? (
              <div className="space-y-2 p-1">
                {[1, 2].map((i) => (
                  <div key={i} className="p-3 rounded-2xl glass-card border border-[#C9B99A]/30 flex items-center gap-3 animate-pulse">
                    <div className="w-10 h-10 rounded-xl bg-[#C9B99A]/40" />
                    <div className="flex-1 space-y-1.5">
                      <div className="h-3.5 bg-[#C9B99A]/40 rounded w-24" />
                      <div className="h-2.5 bg-[#C9B99A]/20 rounded w-36" />
                    </div>
                  </div>
                ))}
              </div>
            ) : groups.length === 0 ? (
              <div className="py-12 px-4 text-center space-y-3">
                <div className="w-12 h-12 mx-auto rounded-2xl bg-[#F0EBE3] flex items-center justify-center text-[#8B7355]">
                  <MessageSquare size={24} />
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-semibold text-[#2E2419]">未加入任何群聊</p>
                  <p className="text-[11px] text-[#A3927C]">点击右上角「+」立即创建你的专属群组</p>
                </div>
                <button
                  onClick={onOpenCreateGroupModal}
                  className="px-4 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold inline-flex items-center gap-1 shadow-warm-sm hover:bg-[#7A6348]"
                >
                  <PlusCircle size={13} />
                  <span>创建群聊</span>
                </button>
              </div>
            ) : (
              groups
                .filter((g) => (g.name || '').toLowerCase().includes(searchTerm.toLowerCase()))
                .map((grp) => {
                  const isActive = activeConversation.id === grp.id;
                  return (
                    <div
                      key={grp.id}
                      onClick={() => {
                        setActiveConversation({
                          id: grp.id,
                          type: 'group',
                          name: grp.name,
                          avatar_url: grp.avatar_url,
                        });
                        onSelectConversation?.();
                      }}
                      className={`p-2.5 rounded-2xl cursor-pointer transition-all flex items-center gap-3 ${
                        isActive
                          ? 'bg-[#8B7355] text-[#FAF8F5] shadow-walnut-glow'
                          : 'glass-card hover:bg-[#F0EBE3] text-[#2E2419]'
                      }`}
                    >
                      <div className="w-10 h-10 rounded-xl bg-[#8B7355]/20 flex items-center justify-center font-bold text-sm text-[#8B7355] shrink-0">
                        <Users size={20} />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between">
                          <h4 className="font-semibold text-sm truncate">{grp.name}</h4>
                        </div>
                        <p className={`text-xs truncate mt-0.5 ${isActive ? 'text-white/80' : 'text-[#A3927C]'}`}>
                          {grp.notice || '欢迎加入群聊交流'}
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
              <div className="py-12 px-4 text-center space-y-3">
                <div className="w-12 h-12 mx-auto rounded-2xl bg-[#F0EBE3] flex items-center justify-center text-[#8B7355]">
                  <Bell size={24} />
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-semibold text-[#2E2419]">暂无待处理申请</p>
                  <p className="text-[11px] text-[#A3927C]">所有好友申请均已处理完成</p>
                </div>
                <button
                  onClick={handleManualRefresh}
                  className="px-3 py-1.5 rounded-xl border border-[#C9B99A]/50 text-xs text-[#8B7355] hover:bg-[#F0EBE3] inline-flex items-center gap-1"
                >
                  <RotateCw size={12} className={refreshing ? 'animate-spin' : ''} />
                  <span>刷新申请</span>
                </button>
              </div>
            ) : (
              pendingRequests.map((req) => (
                <div
                  key={req.user_id}
                  className="p-3 rounded-2xl glass-card border border-[#C9B99A]/40 space-y-2.5 shadow-warm-sm"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-8 h-8 rounded-lg bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] text-white flex items-center justify-center font-bold text-xs shadow-warm-sm">
                        {req.username.slice(0, 1).toUpperCase()}
                      </div>
                      <div>
                        <span className="font-semibold text-xs text-[#2E2419]">{req.username}</span>
                        <p className="text-[10px] text-[#A3927C]">请求添加你为好友</p>
                      </div>
                    </div>
                  </div>

                  {req.remark && (
                    <div className="text-xs text-[#8B7355] bg-[#F0EBE3] p-2 rounded-xl border border-[#C9B99A]/20">
                      <span className="font-medium text-[11px] text-[#A3927C] block mb-0.5">申请附言：</span>
                      "{req.remark}"
                    </div>
                  )}

                  <div className="flex items-center gap-2 pt-1">
                    <button
                      disabled={respondingId === req.user_id}
                      onClick={() => handleRespondRequest(req.user_id, 'accept')}
                      className="flex-1 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center justify-center gap-1 hover:bg-[#7A6348] transition-colors disabled:opacity-50 shadow-warm-sm"
                    >
                      {respondingId === req.user_id ? (
                        <Loader2 size={13} className="animate-spin" />
                      ) : (
                        <Check size={13} />
                      )}
                      <span>同意</span>
                    </button>
                    <button
                      disabled={respondingId === req.user_id}
                      onClick={() => handleRespondRequest(req.user_id, 'reject')}
                      className="flex-1 py-1.5 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-[#8B7355] text-xs font-semibold flex items-center justify-center gap-1 hover:bg-[#EAE4DC] transition-colors disabled:opacity-50"
                    >
                      <X size={13} />
                      <span>拒绝</span>
                    </button>
                  </div>
                </div>
              ))
            )}
          </motion.div>
        )}
      </div>

      {/* 解除好友关系二次确认弹窗 */}
      <AnimatePresence>
        {deleteTarget && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm select-none">
            <motion.div
              initial={{ opacity: 0, scale: 0.92, y: 15 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.92, y: 15 }}
              className="relative w-full max-w-sm p-6 rounded-3xl glass-card shadow-warm-lg space-y-4"
            >
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-2xl bg-rose-100 text-rose-600 flex items-center justify-center shrink-0">
                  <AlertTriangle size={20} />
                </div>
                <div>
                  <h3 className="font-bold text-sm text-[#2E2419]">解除好友关系</h3>
                  <p className="text-xs text-[#A3927C]">此操作将双向解除好友联系</p>
                </div>
              </div>

              <p className="text-xs text-[#4A3B2C] leading-relaxed">
                确定要与好友 <strong className="text-[#8B7355] font-semibold">{deleteTarget.remark || deleteTarget.username}</strong> 解除好友关系吗？解除后双方私聊会话将被重置。
              </p>

              <div className="flex items-center justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setDeleteTarget(null)}
                  disabled={isDeleting}
                  className="px-4 py-2 rounded-xl text-xs font-semibold text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
                >
                  取消
                </button>
                <motion.button
                  whileTap={{ scale: 0.95 }}
                  onClick={handleConfirmDeleteFriend}
                  disabled={isDeleting}
                  className="px-4 py-2 rounded-xl bg-rose-600 hover:bg-rose-700 text-white text-xs font-semibold flex items-center gap-1.5 shadow-warm-sm disabled:opacity-50"
                >
                  {isDeleting ? <Loader2 size={14} className="animate-spin" /> : <UserMinus size={14} />}
                  <span>确认解除</span>
                </motion.button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </aside>
  );
};
