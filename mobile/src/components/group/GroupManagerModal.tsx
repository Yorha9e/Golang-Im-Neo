import React, { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { groupApi, GroupMemberItem, GroupItem } from '../../api';
import { useFriendStore, useAuthStore, useChatStore } from '../../store';
import {
  Users,
  X,
  UserPlus,
  VolumeX,
  Volume2,
  Trash2,
  LogOut,
  Loader2,
} from 'lucide-react';

interface GroupManagerModalProps {
  groupId: string | null;
  onClose: () => void;
}

export const GroupManagerModal: React.FC<GroupManagerModalProps> = ({ groupId, onClose }) => {
  const [group, setGroup] = useState<GroupItem | null>(null);
  const [members, setMembers] = useState<GroupMemberItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [inviteUserId, setInviteUserId] = useState('');
  const [actionLoading, setActionLoading] = useState(false);

  const { friends, fetchGroups } = useFriendStore();
  const { userId } = useAuthStore();
  const { setActiveConversation } = useChatStore();

  useEffect(() => {
    if (groupId) {
      loadData(groupId);
    }
  }, [groupId]);

  const loadData = async (gid: string) => {
    setLoading(true);
    try {
      const [gRes, mRes] = await Promise.all([
        groupApi.getGroupDetail(gid),
        groupApi.getGroupMembers(gid),
      ]);
      if (gRes.code === 0 && gRes.data) setGroup(gRes.data);
      if (mRes.code === 0 && mRes.data) setMembers(mRes.data.members || []);
    } catch (err) {
      console.error('拉取群信息失败:', err);
    } finally {
      setLoading(false);
    }
  };

  if (!groupId) return null;

  const isOwner = group?.owner_id === userId;

  // 邀请好友
  const handleInvite = async () => {
    if (!inviteUserId) return;
    setActionLoading(true);
    try {
      await groupApi.addMember(groupId, inviteUserId);
      setInviteUserId('');
      await loadData(groupId);
    } catch (err) {
      console.error('邀请失败:', err);
    } finally {
      setActionLoading(false);
    }
  };

  // 踢人
  const handleKick = async (targetUid: string) => {
    if (!window.confirm('确认移除该群成员？')) return;
    try {
      await groupApi.kickMember(groupId, targetUid);
      await loadData(groupId);
    } catch (err) {
      console.error('踢人失败:', err);
    }
  };

  // 禁言/解禁 (布尔值切换)
  const handleMute = async (targetUid: string, currentMuted: boolean) => {
    try {
      await groupApi.muteMember(groupId, targetUid, !currentMuted);
      await loadData(groupId);
    } catch (err) {
      console.error('禁言操作失败:', err);
    }
  };

  // 退出群聊
  const handleLeave = async () => {
    if (!window.confirm('确定退出该群聊？')) return;
    try {
      await groupApi.leaveGroup(groupId);
      await fetchGroups();
      setActiveConversation({ id: 'hall', type: 'hall', name: '公共大厅' });
      onClose();
    } catch (err) {
      console.error('退群失败:', err);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
      <motion.div
        initial={{ opacity: 0, scale: 0.92, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.92, y: 15 }}
        className="relative w-full max-w-lg p-6 rounded-3xl glass-card shadow-warm-lg max-h-[85vh] flex flex-col"
      >
        <button
          onClick={onClose}
          className="absolute top-5 right-5 p-1.5 rounded-full text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
        >
          <X size={18} />
        </button>

        {/* 顶部群信息 */}
        <div className="flex items-center gap-3 mb-4 pb-3 border-b border-[#C9B99A]/30">
          <div className="w-12 h-12 rounded-2xl bg-[#8B7355] text-white flex items-center justify-center shadow-warm-sm">
            <Users size={24} />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="font-bold text-base text-[#2E2419] truncate">
              {group?.name || '群聊设置'}
            </h3>
            <p className="text-xs text-[#A3927C] truncate">
              {group?.notice || group?.announcement || '群公告暂无'}
            </p>
          </div>
        </div>

        {/* 邀请好友加入群 */}
        <div className="mb-4">
          <label className="block mb-1.5 text-xs font-semibold text-[#8B7355]">
            邀请好友加入
          </label>
          <div className="flex gap-2">
            <select
              value={inviteUserId}
              onChange={(e) => setInviteUserId(e.target.value)}
              className="flex-1 px-3 py-1.5 rounded-xl glass-input text-xs text-[#2E2419] outline-none"
            >
              <option value="">从好友列表中选择...</option>
              {friends.map((f) => (
                <option key={f.user_id} value={f.user_id}>
                  {f.username}
                </option>
              ))}
            </select>
            <button
              onClick={handleInvite}
              disabled={!inviteUserId || actionLoading}
              className="px-3.5 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1 disabled:opacity-50"
            >
              <UserPlus size={14} />
              <span>邀请</span>
            </button>
          </div>
        </div>

        {/* 成员列表 */}
        <div className="flex-1 overflow-y-auto space-y-2 pr-1 min-h-[160px]">
          <div className="text-xs font-semibold text-[#8B7355] mb-2 flex items-center justify-between">
            <span>群成员 ({members.length})</span>
            {loading && <Loader2 size={13} className="animate-spin" />}
          </div>

          {members.map((m) => {
            const isMuted = !!m.muted;
            return (
              <div
                key={m.user_id}
                className="p-2.5 rounded-2xl glass-input flex items-center justify-between gap-3 text-xs"
              >
                <div className="flex items-center gap-2.5">
                  <div className="w-8 h-8 rounded-xl bg-[#C9B99A] text-white flex items-center justify-center font-bold text-xs">
                    {(m.username || m.user_id).slice(0, 1).toUpperCase()}
                  </div>
                  <div>
                    <div className="flex items-center gap-1.5 font-semibold text-[#2E2419]">
                      <span>{m.username || m.user_id}</span>
                      {m.role === 'owner' && (
                        <span className="text-[10px] px-1 py-0.2 rounded bg-amber-100 text-amber-800 font-normal">
                          群主
                        </span>
                      )}
                    </div>
                    {isMuted && (
                      <span className="text-[10px] text-rose-600">已禁言中</span>
                    )}
                  </div>
                </div>

                {/* 群主操作栏 */}
                {isOwner && m.user_id !== userId && (
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => handleMute(m.user_id, isMuted)}
                      title={isMuted ? '解除禁言' : '设置禁言'}
                      className={`p-1.5 rounded-lg border transition-colors ${
                        isMuted
                          ? 'bg-rose-50 border-rose-200 text-rose-600'
                          : 'hover:bg-[#F0EBE3] text-[#8B7355]'
                      }`}
                    >
                      {isMuted ? <Volume2 size={14} /> : <VolumeX size={14} />}
                    </button>
                    <button
                      onClick={() => handleKick(m.user_id)}
                      title="踢出群聊"
                      className="p-1.5 rounded-lg hover:bg-rose-50 text-stone-400 hover:text-rose-600 transition-colors"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {/* 底部退群按钮 */}
        <div className="pt-4 border-t border-[#C9B99A]/30 flex justify-between items-center mt-2">
          <button
            onClick={handleLeave}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-semibold text-rose-700 bg-rose-50 hover:bg-rose-100 transition-colors"
          >
            <LogOut size={14} />
            <span>退出该群聊</span>
          </button>
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded-xl text-xs font-semibold text-[#8B7355] hover:bg-[#F0EBE3]"
          >
            完成
          </button>
        </div>
      </motion.div>
    </div>
  );
};
