import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Sparkles,
  PenTool,
  Image as ImageIcon,
  Tv,
  ExternalLink,
  MessageCircle,
  RotateCw,
  X,
  Send,
  Loader2,
  Trash2,
  Flame,
  Clock
} from 'lucide-react';
import { profileApi, PostItem, mediaApi } from '../../api';
import { useAuthStore, useFriendStore, Conversation } from '../../store';
import { formatMediaUrl } from '../../utils/media';
import { soundEffects } from '../../audio/soundEffects';

interface TidalBentoViewProps {
  onOpenProfile: (username: string) => void;
  onEnterChat: (conv: Conversation) => void;
  isCreateOpen?: boolean;
  onCloseCreate?: () => void;
}

export const TidalBentoView: React.FC<TidalBentoViewProps> = ({
  onOpenProfile,
  onEnterChat,
  isCreateOpen: externalCreateOpen,
  onCloseCreate,
}) => {
  const { username } = useAuthStore();
  const { friends } = useFriendStore();

  const [posts, setPosts] = useState<PostItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);

  // 发动态模态框状态
  const [internalCreateOpen, setInternalCreateOpen] = useState(false);
  const isCreatePostOpen = externalCreateOpen !== undefined ? externalCreateOpen : internalCreateOpen;
  const setCreatePostOpen = (open: boolean) => {
    if (onCloseCreate && !open) onCloseCreate();
    setInternalCreateOpen(open);
  };

  const [postContent, setPostContent] = useState('');
  const [postMediaType, setPostMediaType] = useState<'text' | 'image' | 'bilibili'>('text');
  const [postMediaUrl, setPostMediaUrl] = useState('');
  const [bilibiliBvid, setBilibiliBvid] = useState('');
  const [uploadingImage, setUploadingImage] = useState(false);
  const [publishing, setPublishing] = useState(false);

  // 汇聚动态流：加载本人主页动态及好友动态
  const loadTidalFeed = async () => {
    setLoading(true);
    soundEffects.playHapticTick();
    try {
      const feedPosts: PostItem[] = [];

      // 1. 加载当前登录用户动态
      if (username) {
        const myRes = await profileApi.getProfile(username);
        if (myRes.code === 0 && myRes.data?.posts) {
          feedPosts.push(...myRes.data.posts);
        }
      }

      // 2. 依次加载最近好友的最新动态 (取前 5 位好友)
      const sampledFriends = friends.slice(0, 5);
      for (const f of sampledFriends) {
        try {
          const fRes = await profileApi.getProfile(f.username);
          if (fRes.code === 0 && fRes.data?.posts) {
            feedPosts.push(...fRes.data.posts);
          }
        } catch {}
      }

      // 3. 去重并按发布时间倒序排列
      const uniquePostsMap = new Map<string | number, PostItem>();
      feedPosts.forEach((p) => uniquePostsMap.set(p.id, p));
      const sorted = Array.from(uniquePostsMap.values()).sort((a, b) => {
        const tA = new Date(a.created_at).getTime() || 0;
        const tB = new Date(b.created_at).getTime() || 0;
        return tB - tA;
      });

      setPosts(sorted);
    } catch (err) {
      console.error('加载动态潮汐失败:', err);
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  };

  useEffect(() => {
    loadTidalFeed();
  }, [username, friends.length]);

  // 图片上传处理
  const handleImageFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setUploadingImage(true);
    soundEffects.playHapticTick();
    try {
      const res = await mediaApi.upload(file, 'image', 'public');
      const url = res.data?.access_url || res.data?.url;
      if (res.code === 0 && url) {
        setPostMediaUrl(url);
        setPostMediaType('image');
      }
    } catch (err) {
      console.error('上传动态图片失败:', err);
    } finally {
      setUploadingImage(false);
    }
  };

  // 发布动态
  const handlePublish = async () => {
    if (!postContent.trim() && !postMediaUrl && !bilibiliBvid) return;

    setPublishing(true);
    soundEffects.playHapticTick();
    try {
      const res = await profileApi.createPost({
        media_type: postMediaType,
        content: postContent.trim(),
        media_url: postMediaType === 'image' ? postMediaUrl : undefined,
        bilibili_bvid: postMediaType === 'bilibili' ? bilibiliBvid.trim() : undefined,
      });

      if (res.code === 0) {
        setPostContent('');
        setPostMediaUrl('');
        setBilibiliBvid('');
        setPostMediaType('text');
        setCreatePostOpen(false);
        soundEffects.playSendSound();
        await loadTidalFeed();
      }
    } catch (err) {
      console.error('发布动态失败:', err);
    } finally {
      setPublishing(false);
    }
  };

  const formatTime = (time: string | number) => {
    try {
      const d = new Date(time);
      return `${d.getMonth() + 1}月${d.getDate()}日 ${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
    } catch {
      return '';
    }
  };

  return (
    <div className="w-full h-full flex flex-col overflow-y-auto px-4 pt-6 pb-28 select-none">
      {/* 1. 顶部标题与行动 */}
      <div className="flex items-center justify-between mb-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-black tracking-tight text-[#2E2419]">
              动态潮汐
            </h1>
            <span className="px-2 py-0.5 text-[10px] font-bold rounded-full bg-[#8B7355]/15 text-[#8B7355] tracking-wider uppercase">
              TIDAL
            </span>
          </div>
          <p className="text-xs text-[#A3927C] mt-0.5 font-medium">
            全域生活与灵感便当盒 · 随时共鸣
          </p>
        </div>

        <div className="flex items-center gap-2">
          {/* 刷新按钮 */}
          <button
            onClick={() => {
              setIsRefreshing(true);
              loadTidalFeed();
            }}
            className="p-2 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-[#8B7355] active:scale-95 transition-transform"
            title="刷新潮汐流"
          >
            <RotateCw size={15} className={isRefreshing ? 'animate-spin' : ''} />
          </button>

          {/* 发布动态按钮 */}
          <button
            onClick={() => {
              soundEffects.playHapticTick();
              setCreatePostOpen(true);
            }}
            className="px-3 py-1.5 rounded-2xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow active:scale-95 transition-transform"
          >
            <PenTool size={13} />
            <span>发潮汐</span>
          </button>
        </div>
      </div>

      {/* 2. 动态便当盒瀑布流 */}
      {loading && posts.length === 0 ? (
        <div className="py-24 flex flex-col items-center justify-center space-y-3">
          <Loader2 size={32} className="animate-spin text-[#8B7355]" />
          <p className="text-xs text-[#A3927C]">正在汇聚潮汐流中...</p>
        </div>
      ) : posts.length === 0 ? (
        <div className="py-16 text-center space-y-3 rounded-3xl bg-[#F0EBE3]/40 border border-dashed border-[#C9B99A]/50 p-6">
          <div className="w-12 h-12 rounded-2xl bg-[#F0EBE3] text-[#8B7355] flex items-center justify-center mx-auto shadow-warm-xs">
            <Sparkles size={24} />
          </div>
          <p className="text-xs font-bold text-[#2E2419]">潮汐静谧无澜</p>
          <p className="text-[11px] text-[#A3927C] max-w-xs mx-auto">
            还没有任何公开动态，快点击右上角率先发布第一条潮汐吧！
          </p>
          <button
            onClick={() => setCreatePostOpen(true)}
            className="px-4 py-1.5 rounded-xl bg-[#8B7355] text-white text-xs font-semibold shadow-warm-sm"
          >
            立即发布动态
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3.5">
          {posts.map((post) => {
            const author = post.username || username || '探索者';
            const isMine = author === username;

            return (
              <motion.div
                key={post.id}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                className="p-4 rounded-3xl bg-[#FAF8F5] border border-[#C9B99A]/40 shadow-warm-sm space-y-3 hover:border-[#8B7355]/40 transition-all group"
              >
                {/* 作者头条 */}
                <div className="flex items-center justify-between">
                  <div
                    onClick={() => onOpenProfile(author)}
                    className="flex items-center gap-2.5 cursor-pointer"
                  >
                    <div className="w-10 h-10 rounded-2xl bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] text-white flex items-center justify-center font-bold text-xs shadow-warm-sm shrink-0">
                      {author.slice(0, 2).toUpperCase()}
                    </div>
                    <div>
                      <div className="flex items-center gap-1.5">
                        <span className="text-xs font-bold text-[#2E2419] hover:text-[#8B7355] transition-colors">
                          @{author}
                        </span>
                        {isMine && (
                          <span className="text-[9px] px-1.5 py-0.2 rounded bg-[#8B7355]/15 text-[#8B7355] font-semibold">
                            我
                          </span>
                        )}
                      </div>
                      <span className="text-[10px] text-[#A3927C] font-mono flex items-center gap-1">
                        <Clock size={10} />
                        <span>{formatTime(post.created_at)}</span>
                      </span>
                    </div>
                  </div>

                  {/* 快捷私聊触碰 */}
                  {!isMine && (
                    <button
                      onClick={() => {
                        soundEffects.playHapticTick();
                        onEnterChat({
                          id: post.user_id || author,
                          type: 'private',
                          name: author,
                        });
                      }}
                      className="p-2 rounded-xl bg-[#F0EBE3] hover:bg-[#8B7355] text-[#8B7355] hover:text-white transition-all shadow-warm-xs"
                      title={`发私聊给 @${author}`}
                    >
                      <MessageCircle size={14} />
                    </button>
                  )}
                </div>

                {/* 动态文字主体 */}
                {post.content && (
                  <p className="text-xs leading-relaxed text-[#2E2419] whitespace-pre-wrap select-text px-1">
                    {post.content}
                  </p>
                )}

                {/* 动态配图 */}
                {post.media_url && (
                  <div
                    onClick={() => setLightboxUrl(formatMediaUrl(post.media_url!))}
                    className="rounded-2xl overflow-hidden border border-[#C9B99A]/30 bg-[#F0EBE3]/50 flex items-center justify-center cursor-zoom-in p-1 transition-all hover:border-[#8B7355]/60 hover:shadow-warm-md"
                  >
                    <img
                      src={formatMediaUrl(post.media_url)}
                      alt="配图"
                      className="w-full max-h-[300px] object-contain rounded-xl transition-transform active:scale-95"
                    />
                  </div>
                )}

                {/* B 站专属卡片 */}
                {post.media_type === 'bilibili' && post.bilibili_bvid && (
                  <a
                    href={`https://www.bilibili.com/video/${post.bilibili_bvid}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-3 p-3 rounded-2xl bg-[#F0EBE3]/70 border border-[#C9B99A]/40 hover:bg-[#EAE4DC] transition-all"
                  >
                    <div className="w-9 h-9 rounded-xl bg-[#FB7299] text-white flex items-center justify-center shrink-0 shadow-sm">
                      <Tv size={18} />
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="text-[10px] font-mono px-1 py-0.5 rounded bg-[#FB7299]/15 text-[#FB7299] font-bold">
                          {post.bilibili_bvid}
                        </span>
                      </div>
                      <p className="text-xs font-semibold text-[#2E2419] truncate mt-0.5">
                        点击前往 B 站观看视频
                      </p>
                    </div>
                    <ExternalLink size={14} className="text-[#A3927C]" />
                  </a>
                )}
              </motion.div>
            );
          })}
        </div>
      )}

      {/* 3. 发动态流体弹窗 */}
      <AnimatePresence>
        {isCreatePostOpen && (
          <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center p-0 sm:p-4 bg-black/50 backdrop-blur-sm">
            <motion.div
              initial={{ y: '100%' }}
              animate={{ y: 0 }}
              exit={{ y: '100%' }}
              transition={{ type: 'spring', stiffness: 400, damping: 30 }}
              className="w-full max-w-lg bg-[#FAF8F5] rounded-t-3xl sm:rounded-3xl p-5 border border-[#C9B99A]/40 shadow-warm-xl space-y-4"
            >
              <div className="flex items-center justify-between pb-3 border-b border-[#C9B99A]/30">
                <div className="flex items-center gap-2">
                  <Sparkles size={16} className="text-[#8B7355]" />
                  <h3 className="font-bold text-sm text-[#2E2419]">
                    记录当下的潮汐微光
                  </h3>
                </div>
                <button
                  onClick={() => setCreatePostOpen(false)}
                  className="p-1 rounded-xl text-[#A3927C] hover:text-[#2E2419]"
                >
                  <X size={18} />
                </button>
              </div>

              {/* 文本输入 */}
              <textarea
                value={postContent}
                onChange={(e) => setPostContent(e.target.value)}
                placeholder="分享你的灵感、代码心得或今日心情..."
                rows={4}
                className="w-full p-3 rounded-2xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none resize-none shadow-warm-xs"
              />

              {/* 媒体类型选择 */}
              <div className="flex items-center gap-2">
                <label className="px-3 py-1.5 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 text-xs font-semibold text-[#8B7355] flex items-center gap-1.5 cursor-pointer hover:bg-[#EAE4DC] transition-colors">
                  <ImageIcon size={14} />
                  <span>{uploadingImage ? '上传中...' : '添加图片'}</span>
                  <input
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={handleImageFileChange}
                    disabled={uploadingImage}
                  />
                </label>

                <button
                  type="button"
                  onClick={() => {
                    setPostMediaType(postMediaType === 'bilibili' ? 'text' : 'bilibili');
                  }}
                  className={`px-3 py-1.5 rounded-xl border text-xs font-semibold flex items-center gap-1.5 transition-colors ${
                    postMediaType === 'bilibili'
                      ? 'bg-[#FB7299] text-white border-[#FB7299]'
                      : 'bg-[#F0EBE3] text-[#8B7355] border-[#C9B99A]/40'
                  }`}
                >
                  <Tv size={14} />
                  <span>B站视频</span>
                </button>
              </div>

              {/* B 站 BV 号输入框 */}
              {postMediaType === 'bilibili' && (
                <input
                  type="text"
                  value={bilibiliBvid}
                  onChange={(e) => setBilibiliBvid(e.target.value)}
                  placeholder="请输入 BV 号 (例如: BV1xx411c7mD)"
                  className="w-full px-3 py-2 rounded-xl glass-input text-xs text-[#2E2419] outline-none"
                />
              )}

              {/* 图片已上传预览 */}
              {postMediaUrl && (
                <div className="relative inline-block">
                  <img
                    src={formatMediaUrl(postMediaUrl)}
                    alt="待发布配图"
                    className="w-20 h-20 rounded-xl object-cover border border-[#C9B99A]/40"
                  />
                  <button
                    onClick={() => {
                      setPostMediaUrl('');
                      setPostMediaType('text');
                    }}
                    className="absolute -top-1.5 -right-1.5 p-1 rounded-full bg-black/60 text-white hover:bg-black"
                  >
                    <X size={12} />
                  </button>
                </div>
              )}

              {/* 底部发布按钮 */}
              <div className="pt-2 flex justify-end gap-2">
                <button
                  onClick={() => setCreatePostOpen(false)}
                  className="px-4 py-2 rounded-xl text-xs font-semibold text-[#A3927C] hover:text-[#2E2419]"
                >
                  取消
                </button>
                <button
                  disabled={publishing || (!postContent.trim() && !postMediaUrl && !bilibiliBvid)}
                  onClick={handlePublish}
                  className="px-5 py-2 rounded-xl bg-[#8B7355] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-50 transition-all"
                >
                  {publishing ? (
                    <Loader2 size={14} className="animate-spin" />
                  ) : (
                    <Send size={14} />
                  )}
                  <span>发布</span>
                </button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {/* 4. 图片放大全屏预览 */}
      <AnimatePresence>
        {lightboxUrl && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => setLightboxUrl(null)}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-md cursor-zoom-out"
          >
            <motion.div
              initial={{ scale: 0.85 }}
              animate={{ scale: 1 }}
              exit={{ scale: 0.85 }}
              className="relative max-w-full max-h-[85vh] rounded-3xl overflow-hidden glass-card shadow-warm-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <img
                src={formatMediaUrl(lightboxUrl)}
                alt="放大预览图"
                className="w-full h-full object-contain max-h-[85vh]"
              />
              <button
                onClick={() => setLightboxUrl(null)}
                className="absolute top-3 right-3 p-2 rounded-full bg-black/50 text-white hover:bg-black/70 transition-colors"
              >
                <X size={18} />
              </button>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};
