import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { profileApi, UserProfile } from '../../api';
import { useAuthStore, useFriendStore, useChatStore } from '../../store';
import {
  X,
  User,
  Sparkles,
  MessageSquare,
  UserPlus,
  Tv,
  ExternalLink,
  Loader2,
} from 'lucide-react';

interface UserProfileModalProps {
  username: string | null;
  onClose: () => void;
  onOpenAddFriend?: (username: string) => void;
}

export const UserProfileModal: React.FC<UserProfileModalProps> = ({
  username,
  onClose,
  onOpenAddFriend,
}) => {
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [loading, setLoading] = useState(false);

  const { username: currentUsername } = useAuthStore();
  const { friends } = useFriendStore();
  const { setActiveConversation } = useChatStore();

  useEffect(() => {
    if (username) {
      loadUserProfile(username);
    } else {
      setProfile(null);
    }
  }, [username]);

  const loadUserProfile = async (uname: string) => {
    setLoading(true);
    try {
      const res = await profileApi.getProfile(uname);
      if (res.code === 0 && res.data) {
        setProfile(res.data);
      }
    } catch (err) {
      console.error('拉取用户主页失败:', err);
    } finally {
      setLoading(false);
    }
  };

  if (!username) return null;

  const isSelf = username === currentUsername;
  const isFriend = friends.some((f) => f.username === username);
  const friendData = friends.find((f) => f.username === username);

  // 发起私聊
  const handleStartChat = () => {
    if (friendData) {
      setActiveConversation({
        id: friendData.user_id,
        type: 'private',
        name: friendData.username,
        avatar_url: friendData.avatar_url,
      });
      onClose();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.92, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.92, y: 20 }}
        transition={{ type: 'spring', stiffness: 350, damping: 26 }}
        className="relative w-full max-w-lg p-6 rounded-3xl glass-card shadow-warm-lg max-h-[85vh] flex flex-col overflow-hidden"
      >
        {/* 右上角关闭按钮 */}
        <button
          onClick={onClose}
          className="absolute top-5 right-5 p-1.5 rounded-full text-[#8B7355] hover:bg-[#F0EBE3] transition-colors z-20 shadow-warm-sm"
        >
          <X size={18} />
        </button>

        {loading ? (
          <div className="py-24 flex flex-col items-center justify-center space-y-3">
            <Loader2 size={32} className="animate-spin text-[#8B7355]" />
            <p className="text-xs text-[#A3927C]">正在加载 {username} 的个人主页...</p>
          </div>
        ) : (
          <>
            {/* 1. 顶部个人资料看板 */}
            <div className="flex items-start gap-4 pb-5 border-b border-[#C9B99A]/30 shrink-0 pr-8">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] text-white flex items-center justify-center text-2xl font-bold shadow-walnut-glow overflow-hidden shrink-0">
                {profile?.avatar_url ? (
                  <img src={profile.avatar_url} alt="头像" className="w-full h-full object-cover" />
                ) : (
                  username.slice(0, 1).toUpperCase()
                )}
              </div>

              <div className="flex-1 min-w-0">
                {/* 用户名与 @id 并排展示 */}
                <div className="flex items-center flex-wrap gap-2">
                  <h3 className="font-bold text-base text-[#2E2419] truncate">
                    {profile?.nickname || profile?.username || username}
                  </h3>
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-[#8B7355]/15 text-[#8B7355] font-medium">
                    @{profile?.user_id || username}
                  </span>
                </div>

                <p className="text-xs text-[#8B7355] mt-1 line-clamp-2">
                  {profile?.signature || '「这个人很神秘，还没有填写个性签名。」'}
                </p>

                {/* 快捷操作按钮 */}
                <div className="flex items-center gap-2 mt-3">
                  {!isSelf && isFriend && (
                    <button
                      onClick={handleStartChat}
                      className="px-3 py-1 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1.5 shadow-warm-sm hover:bg-[#7A6348] transition-colors"
                    >
                      <MessageSquare size={13} />
                      <span>发起私聊</span>
                    </button>
                  )}

                  {!isSelf && !isFriend && (
                    <button
                      onClick={() => {
                        onClose();
                        onOpenAddFriend?.(username);
                      }}
                      className="px-3 py-1 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1.5 shadow-warm-sm hover:bg-[#7A6348] transition-colors"
                    >
                      <UserPlus size={13} />
                      <span>加为好友</span>
                    </button>
                  )}
                </div>
              </div>
            </div>

            {/* 2. 动态流列表 */}
            <div className="flex-1 overflow-y-auto space-y-3 pt-4 pr-1">
              <div className="flex items-center justify-between text-xs font-semibold text-[#8B7355] px-1 mb-2">
                <span className="flex items-center gap-1.5">
                  <Sparkles size={14} />
                  <span>发布的动态 ({profile?.posts?.length || 0})</span>
                </span>
              </div>

              {profile?.posts?.length === 0 ? (
                <div className="py-16 text-center text-xs text-[#A3927C]">
                  该用户暂未发布过任何公开动态
                </div>
              ) : (
                profile?.posts?.map((post) => (
                  <div
                    key={post.id}
                    className="p-4 rounded-2xl glass-input space-y-2.5 text-xs text-[#4A3B2C]"
                  >
                    {/* 动态内容 */}
                    {post.content && (
                      <p className="leading-relaxed whitespace-pre-wrap">
                        {post.content}
                      </p>
                    )}

                    {/* 配图 */}
                    {post.media_url && (
                      <div className="rounded-xl overflow-hidden max-h-48 border border-[#C9B99A]/30">
                        <img src={post.media_url} alt="动态图" className="w-full h-full object-cover" />
                      </div>
                    )}

                    {/* B 站卡片 */}
                    {post.media_type === 'bilibili' && post.bilibili_bvid && (
                      <a
                        href={`https://www.bilibili.com/video/${post.bilibili_bvid}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center gap-2.5 p-2.5 rounded-xl bg-[#F0EBE3] hover:bg-[#EAE4DC] transition-all border border-[#C9B99A]/40"
                      >
                        <div className="w-8 h-8 rounded-lg bg-[#FB7299] text-white flex items-center justify-center shrink-0">
                          <Tv size={16} />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="font-semibold text-xs text-[#2E2419] truncate">
                            Bilibili · {post.bilibili_bvid}
                          </div>
                          <p className="text-[10px] text-[#A3927C] truncate">
                            点击打开哔哩哔哩观看原视频
                          </p>
                        </div>
                        <ExternalLink size={14} className="text-[#8B7355] mr-1" />
                      </a>
                    )}

                    {/* 时间 */}
                    <div className="text-[10px] text-[#A3927C] pt-1">
                      {new Date(post.created_at).toLocaleString()}
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        )}
      </motion.div>
    </div>
  );
};
