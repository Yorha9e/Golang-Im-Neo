import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { profileApi, UserProfile } from '../../api';
import { useAuthStore, useFriendStore, useChatStore } from '../../store';
import { RoleBadge } from '../common/RoleBadge';
import {
  X,
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
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);

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

  const displayUser = profile?.user || profile;
  const avatarUrl = displayUser?.avatar_url || '';
  const postList = profile?.posts || [];

  const formatTime = (timeVal: any) => {
    if (!timeVal) return '';
    const d = typeof timeVal === 'number' ? new Date(timeVal) : new Date(timeVal);
    return isNaN(d.getTime()) ? '' : d.toLocaleString();
  };

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
                {avatarUrl ? (
                  <img src={avatarUrl} alt="头像" className="w-full h-full object-cover" />
                ) : (
                  username.slice(0, 1).toUpperCase()
                )}
              </div>

              <div className="flex-1 min-w-0">
                {/* 用户名、身份牌与 @id 并排展示 */}
                <div className="flex items-center flex-wrap gap-2">
                  <h3 className="font-bold text-base text-[#2E2419] truncate">
                    {displayUser?.nickname || displayUser?.username || username}
                  </h3>
                  <RoleBadge username={displayUser?.username || username} size="sm" />
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-[#8B7355]/15 text-[#8B7355] font-medium">
                    @{displayUser?.user_id || username}
                  </span>
                </div>

                <p className="text-xs text-[#8B7355] mt-1 line-clamp-2">
                  {displayUser?.signature || '「这个人很神秘，还没有填写个性签名。」'}
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
                  <span>发布的动态 ({postList.length})</span>
                </span>
              </div>

              {postList.length === 0 ? (
                <div className="py-16 text-center text-xs text-[#A3927C]">
                  该用户暂未发布过任何公开动态
                </div>
              ) : (
                postList.map((post) => (
                  <div
                    key={post.id}
                    className="p-4 rounded-2xl glass-input space-y-2.5 text-xs text-[#4A3B2C]"
                  >
                    {/* 动态内容 */}
                    {post.content && (
                      <p className="leading-relaxed whitespace-pre-wrap select-text">
                        {post.content}
                      </p>
                    )}

                    {/* 配图 */}
                    {post.media_url && (
                      <div
                        onClick={() => setLightboxUrl(post.media_url!)}
                        className="rounded-xl overflow-hidden border border-[#C9B99A]/30 bg-[#F0EBE3]/50 flex items-center justify-center cursor-zoom-in group/img p-1 transition-all hover:border-[#8B7355]/60"
                      >
                        <img
                          src={post.media_url}
                          alt="动态图"
                          className="w-full max-h-[320px] object-contain rounded-lg transition-transform group-hover/img:scale-[1.01]"
                        />
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
                      {formatTime(post.created_at)}
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        )}
      </motion.div>

      {/* 3. 全屏大图灯箱预览 */}
      <AnimatePresence>
        {lightboxUrl && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => setLightboxUrl(null)}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-8 bg-black/75 backdrop-blur-md cursor-zoom-out select-none"
          >
            <motion.div
              initial={{ scale: 0.9, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.9, opacity: 0 }}
              transition={{ type: 'spring', stiffness: 350, damping: 28 }}
              className="relative max-w-5xl max-h-[90vh] rounded-3xl overflow-hidden glass-card shadow-warm-lg"
              onClick={(e) => e.stopPropagation()}
            >
              <img
                src={lightboxUrl}
                alt="大图全屏预览"
                className="w-full h-full object-contain max-h-[85vh] rounded-2xl"
              />
              <button
                onClick={() => setLightboxUrl(null)}
                className="absolute top-4 right-4 p-2 rounded-full bg-black/50 hover:bg-black/70 text-white transition-colors shadow-lg"
                title="关闭预览"
              >
                <X size={20} />
              </button>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};
