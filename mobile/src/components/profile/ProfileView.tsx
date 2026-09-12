import React, { useEffect, useState, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useAuthStore } from '../../store';
import { profileApi, mediaApi, UserProfile, PostItem } from '../../api';
import { RoleBadge } from '../common/RoleBadge';
import { formatMediaUrl } from '../../utils/media';
import {
  User,
  Edit3,
  Camera,
  Send,
  Trash2,
  ExternalLink,
  Sparkles,
  Tv,
  Image as ImageIcon,
  Loader2,
  CheckCircle2,
  X,
  ZoomIn,
} from 'lucide-react';

export const ProfileView: React.FC = () => {
  const { username, userId } = useAuthStore();
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [loading, setLoading] = useState(true);

  // 编辑资料状态
  const [isEditingProfile, setIsEditingProfile] = useState(false);
  const [nickname, setNickname] = useState('');
  const [signature, setSignature] = useState('');
  const [savingProfile, setSavingProfile] = useState(false);
  const avatarInputRef = useRef<HTMLInputElement | null>(null);

  // 发布动态状态
  const [postContent, setPostContent] = useState('');
  const [postType, setPostType] = useState<'text' | 'image' | 'bilibili'>('text');
  const [bilibiliBvid, setBilibiliBvid] = useState('');
  const [postImageUrl, setPostImageUrl] = useState('');
  const [publishing, setPublishing] = useState(false);
  const postImageInputRef = useRef<HTMLInputElement | null>(null);

  // 图片全屏大图灯箱预览
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);

  useEffect(() => {
    if (username) {
      loadProfile();
    }
  }, [username]);

  const loadProfile = async () => {
    setLoading(true);
    try {
      const res = await profileApi.getProfile(username);
      if (res.code === 0 && res.data) {
        setProfile(res.data);
        setNickname(res.data.user?.nickname || res.data.nickname || '');
        setSignature(res.data.user?.signature || res.data.signature || '');
      }
    } catch (err) {
      console.error('拉取个人主页失败:', err);
    } finally {
      setLoading(false);
    }
  };

  // 更换头像
  const handleAvatarChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    try {
      const uploadRes = await mediaApi.upload(file, 'avatar', 'public');
      const avatarUrl = uploadRes.data?.access_url || uploadRes.data?.url;
      if (uploadRes.code === 0 && avatarUrl) {
        await profileApi.updateProfile({ avatar_url: avatarUrl });
        useAuthStore.getState().setAvatarUrl(avatarUrl);
        await loadProfile();
      }
    } catch (err) {
      console.error('更换头像失败:', err);
    }
  };

  // 保存资料修改
  const handleSaveProfile = async () => {
    setSavingProfile(true);
    try {
      await profileApi.updateProfile({ nickname, signature });
      if (nickname) {
        useAuthStore.getState().setNickname(nickname);
      }
      setIsEditingProfile(false);
      await loadProfile();
    } catch (err) {
      console.error('保存资料失败:', err);
    } finally {
      setSavingProfile(false);
    }
  };

  // 上传动态图片
  const handlePostImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    try {
      const res = await mediaApi.upload(file, 'image', 'public');
      const url = res.data?.access_url || res.data?.url;
      if (res.code === 0 && url) {
        setPostImageUrl(url);
      }
    } catch (err) {
      console.error('上传动态图片失败:', err);
    }
  };

  // 发布动态
  const handleCreatePost = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!postContent.trim() && !bilibiliBvid && !postImageUrl) return;

    // 校验 B 站 BV 号格式
    if (postType === 'bilibili') {
      const bvidRegex = /^BV[a-zA-Z0-9]{10}$/;
      if (!bvidRegex.test(bilibiliBvid.trim())) {
        alert('请输入合法的 12 位 B 站 BV 号 (如: BV1xx411c7mD)');
        return;
      }
    }

    setPublishing(true);
    try {
      const res = await profileApi.createPost({
        media_type: postType,
        content: postContent.trim(),
        media_url: postImageUrl,
        bilibili_bvid: bilibiliBvid.trim(),
      });
      if (res.code === 0) {
        setPostContent('');
        setBilibiliBvid('');
        setPostImageUrl('');
        setPostType('text');
        await loadProfile();
      } else {
        alert(res.msg || '发布失败');
      }
    } catch (err: any) {
      console.error('发布动态失败:', err);
      alert(err.message || '发布动态失败');
    } finally {
      setPublishing(false);
    }
  };

  // 删除动态
  const handleDeletePost = async (postId: string | number) => {
    if (!window.confirm('确认删除该动态？')) return;
    try {
      await profileApi.deletePost(String(postId));
      await loadProfile();
    } catch (err) {
      console.error('删除动态失败:', err);
    }
  };

  const formatTime = (timeVal: any) => {
    if (!timeVal) return '';
    const d = typeof timeVal === 'number' ? new Date(timeVal) : new Date(timeVal);
    return isNaN(d.getTime()) ? '' : d.toLocaleString();
  };

  const displayUser = profile?.user || profile;
  const avatarUrl = displayUser?.avatar_url || '';
  const postList = profile?.posts || [];

  return (
    <div className="flex-1 h-full overflow-y-auto bg-[#FAF8F5] p-3.5 sm:p-6 lg:p-10 pb-32 select-none">
      <div className="max-w-4xl mx-auto space-y-5 sm:space-y-8">
        {/* 1. 个人资料顶部看板 */}
        <motion.div
          initial={{ opacity: 0, y: 15 }}
          animate={{ opacity: 1, y: 0 }}
          className="relative p-5 sm:p-8 rounded-3xl glass-card shadow-warm-md overflow-hidden"
        >
          {/* 背景暖阳微光 */}
          <div className="absolute -top-16 -right-16 w-64 h-64 bg-[#C9B99A]/25 rounded-full blur-3xl pointer-events-none" />

          <div className="flex flex-col sm:flex-row items-center sm:items-start gap-6 relative z-10">
            {/* 头像与更换触发 */}
            <div className="relative group">
              <input
                ref={avatarInputRef}
                type="file"
                accept="image/*"
                className="hidden"
                onChange={handleAvatarChange}
              />
              <div className="w-24 h-24 rounded-3xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] flex items-center justify-center text-white text-3xl font-bold shadow-walnut-glow overflow-hidden">
                {avatarUrl ? (
                  <img src={formatMediaUrl(avatarUrl)} alt="头像" className="w-full h-full object-cover" />
                ) : (
                  username.slice(0, 1).toUpperCase()
                )}
              </div>
              <button
                onClick={() => avatarInputRef.current?.click()}
                className="absolute inset-0 rounded-3xl bg-black/40 text-white flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"
                title="更换头像"
              >
                <Camera size={22} />
              </button>
            </div>

            {/* 用户文字详情与修改 */}
            <div className="flex-1 text-center sm:text-left min-w-0">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <div className="flex items-center justify-center sm:justify-start gap-2 flex-wrap">
                    <h2 className="text-xl font-bold text-[#2E2419]">
                      {displayUser?.nickname || displayUser?.username || username}
                    </h2>
                    <RoleBadge role={useAuthStore.getState().role} username={username} size="md" />
                    <span className="text-[11px] font-mono px-2 py-0.5 rounded-md bg-[#8B7355]/15 text-[#8B7355] font-semibold">
                      @{username}
                    </span>
                  </div>
                  <p className="text-xs font-mono text-[#A3927C] mt-1">
                    UID: {userId || 'u_neo'}
                  </p>
                </div>

                <button
                  onClick={() => setIsEditingProfile(!isEditingProfile)}
                  className="px-4 py-1.5 rounded-xl border border-[#C9B99A]/50 text-xs font-semibold text-[#8B7355] hover:bg-[#F0EBE3] transition-colors self-center sm:self-auto flex items-center gap-1.5 shadow-warm-sm"
                >
                  <Edit3 size={14} />
                  <span>{isEditingProfile ? '取消编辑' : '编辑个人资料'}</span>
                </button>
              </div>

              {/* 资料编辑表单 vs 签名展示 */}
              {isEditingProfile ? (
                <div className="mt-4 p-4 rounded-2xl bg-[#F0EBE3]/80 border border-[#C9B99A]/40 space-y-3">
                  <div>
                    <label className="block text-[11px] font-semibold text-[#8B7355] mb-1">
                      昵称
                    </label>
                    <input
                      type="text"
                      value={nickname}
                      onChange={(e) => setNickname(e.target.value)}
                      placeholder="设置你的专属昵称"
                      className="w-full px-3 py-1.5 rounded-xl glass-input text-xs text-[#2E2419] outline-none"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-semibold text-[#8B7355] mb-1">
                      个性签名
                    </label>
                    <input
                      type="text"
                      value={signature}
                      onChange={(e) => setSignature(e.target.value)}
                      placeholder="写一句温暖的话语..."
                      className="w-full px-3 py-1.5 rounded-xl glass-input text-xs text-[#2E2419] outline-none"
                    />
                  </div>
                  <div className="flex justify-end pt-1">
                    <button
                      onClick={handleSaveProfile}
                      disabled={savingProfile}
                      className="px-4 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1 shadow-warm-sm"
                    >
                      {savingProfile && <Loader2 size={13} className="animate-spin" />}
                      <span>保存修改</span>
                    </button>
                  </div>
                </div>
              ) : (
                <p className="mt-3 text-sm text-[#8B7355] bg-[#F0EBE3]/50 p-2.5 rounded-xl border border-[#C9B99A]/20 inline-block">
                  {displayUser?.signature || '「在这个快节奏的世界里，愿有一处角落静谧如暖阳。」'}
                </p>
              )}
            </div>
          </div>
        </motion.div>

        {/* 2. 动态发布器 */}
        <motion.div
          initial={{ opacity: 0, y: 15 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.1 }}
          className="p-6 rounded-3xl glass-card shadow-warm-sm space-y-4"
        >
          <div className="flex items-center justify-between border-b border-[#C9B99A]/20 pb-3">
            <h3 className="font-bold text-sm text-[#2E2419] flex items-center gap-2">
              <Sparkles size={16} className="text-[#8B7355]" />
              <span>分享新鲜动态</span>
            </h3>

            {/* 类型切换 */}
            <div className="flex items-center p-1 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-xs">
              <button
                onClick={() => setPostType('text')}
                className={`px-3 py-1 rounded-lg font-semibold transition-all ${
                  postType === 'text' ? 'bg-[#8B7355] text-white shadow-warm-sm' : 'text-[#8B7355]'
                }`}
              >
                文本
              </button>
              <button
                onClick={() => setPostType('image')}
                className={`px-3 py-1 rounded-lg font-semibold transition-all ${
                  postType === 'image' ? 'bg-[#8B7355] text-white shadow-warm-sm' : 'text-[#8B7355]'
                }`}
              >
                图文
              </button>
              <button
                onClick={() => setPostType('bilibili')}
                className={`px-3 py-1 rounded-lg font-semibold transition-all ${
                  postType === 'bilibili' ? 'bg-[#8B7355] text-white shadow-warm-sm' : 'text-[#8B7355]'
                }`}
              >
                B站视频
              </button>
            </div>
          </div>

          <form onSubmit={handleCreatePost} className="space-y-3">
            <textarea
              value={postContent}
              onChange={(e) => setPostContent(e.target.value)}
              placeholder="分享你此刻的想法、技术心得或日常..."
              rows={3}
              className="w-full p-3 rounded-2xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none resize-none select-text"
            />

            {/* 图文模式：图片选择 */}
            {postType === 'image' && (
              <div className="flex items-center gap-3">
                <input
                  ref={postImageInputRef}
                  type="file"
                  accept="image/*"
                  className="hidden"
                  onChange={handlePostImageUpload}
                />
                <button
                  type="button"
                  onClick={() => postImageInputRef.current?.click()}
                  className="px-3 py-2 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-xs text-[#8B7355] font-semibold flex items-center gap-1.5"
                >
                  <ImageIcon size={16} />
                  <span>{postImageUrl ? '重新选择图片' : '上传动态配图'}</span>
                </button>
                {postImageUrl && (
                  <span className="text-[11px] text-emerald-700 flex items-center gap-1">
                    <CheckCircle2 size={13} />
                    图片已就绪
                  </span>
                )}
              </div>
            )}

            {/* B站视频模式：输入 BV 号 */}
            {postType === 'bilibili' && (
              <div className="flex items-center gap-2">
                <div className="relative flex-1">
                  <Tv size={16} className="absolute left-3.5 top-2.5 text-[#FB7299]" />
                  <input
                    type="text"
                    value={bilibiliBvid}
                    onChange={(e) => setBilibiliBvid(e.target.value)}
                    placeholder="输入 12 位 BV 号 (例如: BV1xx411c7mD)"
                    className="w-full pl-10 pr-4 py-2 rounded-xl glass-input text-xs text-[#2E2419] outline-none font-mono"
                  />
                </div>
              </div>
            )}

            <div className="flex justify-end pt-1">
              <motion.button
                whileHover={{ scale: 1.03 }}
                whileTap={{ scale: 0.95 }}
                type="submit"
                disabled={publishing || (!postContent.trim() && !bilibiliBvid && !postImageUrl)}
                className="px-5 py-2 rounded-xl bg-gradient-to-r from-[#8B7355] to-[#7A6348] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-50"
              >
                {publishing && <Loader2 size={14} className="animate-spin" />}
                <span>发布动态</span>
                <Send size={13} />
              </motion.button>
            </div>
          </form>
        </motion.div>

        {/* 3. 动态时间线列表 */}
        <div className="space-y-4">
          <h3 className="font-bold text-sm text-[#2E2419] px-1">
            历史动态 ({postList.length})
          </h3>

          {postList.length === 0 ? (
            <div className="py-16 text-center text-xs text-[#A3927C] glass-card rounded-3xl">
              还没有发布过任何动态，快在上方记录你的精彩一刻吧！
            </div>
          ) : (
            postList.map((post) => {
              const authorName = post.username || displayUser?.username || username || '我';
              return (
                <motion.div
                  key={post.id}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  className="p-6 rounded-3xl glass-card shadow-warm-sm space-y-3 relative group"
                >
                  {/* 动态头部 */}
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-xl bg-[#8B7355] text-white flex items-center justify-center font-bold text-xs shadow-warm-sm">
                        {authorName.slice(0, 1).toUpperCase()}
                      </div>
                      <div>
                        <h4 className="font-semibold text-xs text-[#2E2419]">{authorName}</h4>
                        <p className="text-[10px] text-[#A3927C]">{formatTime(post.created_at)}</p>
                      </div>
                    </div>

                    {/* 作者删除按钮 */}
                    <button
                      onClick={() => handleDeletePost(post.id)}
                      title="删除此动态"
                      className="p-1.5 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-colors opacity-0 group-hover:opacity-100"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>

                  {/* 动态文本 */}
                  {post.content && (
                    <p className="text-xs leading-relaxed text-[#4A3B2C] whitespace-pre-wrap select-text">
                      {post.content}
                    </p>
                  )}

                  {/* 图文模式配图 (自适应完整展示 + 点击全屏灯箱) */}
                  {post.media_url && (
                    <div
                      onClick={() => setLightboxUrl(formatMediaUrl(post.media_url!))}
                      className="rounded-2xl overflow-hidden border border-[#C9B99A]/30 bg-[#F0EBE3]/50 flex items-center justify-center cursor-zoom-in group/img transition-all hover:border-[#8B7355]/60 hover:shadow-warm-md p-1"
                    >
                      <img
                        src={formatMediaUrl(post.media_url)}
                        alt="动态配图"
                        className="w-full max-h-[420px] object-contain rounded-xl transition-transform group-hover/img:scale-[1.01]"
                      />
                    </div>
                  )}

                  {/* B 站专属卡片预览 */}
                  {post.media_type === 'bilibili' && post.bilibili_bvid && (
                    <a
                      href={`https://www.bilibili.com/video/${post.bilibili_bvid}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-3 p-3 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 hover:bg-[#EAE4DC] transition-all group/card shadow-warm-sm"
                    >
                      <div className="w-10 h-10 rounded-xl bg-[#FB7299] text-white flex items-center justify-center shrink-0">
                        <Tv size={20} />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-1.5">
                          <span className="text-[10px] font-mono px-1 py-0.5 rounded bg-[#FB7299]/15 text-[#FB7299] font-bold">
                            {post.bilibili_bvid}
                          </span>
                          <span className="text-xs font-semibold text-[#2E2419] truncate">
                            哔哩哔哩视频内容卡片
                          </span>
                        </div>
                        <p className="text-[11px] text-[#A3927C] truncate mt-0.5">
                          点击在新标签页中打开 Bilibili 观看原视频
                        </p>
                      </div>
                      <ExternalLink size={16} className="text-[#8B7355] group-hover/card:translate-x-0.5 transition-transform mr-1" />
                    </a>
                  )}
                </motion.div>
              );
            })
          )}
        </div>
      </div>

      {/* 4. 全屏大图灯箱预览 */}
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
