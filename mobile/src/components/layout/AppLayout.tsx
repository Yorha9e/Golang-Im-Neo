import React, { useEffect, useState } from 'react';
import { useAuthStore, useAudioStore, useFriendStore, useChatStore } from '../../store';
import { wsClient, ConnectionStatus } from '../../socket/wsClient';
import { RoleBadge } from '../common/RoleBadge';
import { formatMediaUrl } from '../../utils/media';
import {
  Sparkles,
  Volume2,
  VolumeX,
  LogOut,
  Shield,
  Bell,
} from 'lucide-react';
import { motion } from 'framer-motion';

interface AppLayoutProps {
  children: React.ReactNode;
  activeView: 'chat' | 'profile';
  setActiveView: (view: 'chat' | 'profile') => void;
  onOpenAdminDashboard?: () => void;
  onOpenNoticeDrawer?: () => void;
}

export const AppLayout: React.FC<AppLayoutProps> = ({
  children,
  activeView,
  setActiveView,
  onOpenAdminDashboard,
  onOpenNoticeDrawer,
}) => {
  const { username, role, avatarUrl, nickname, logout } = useAuthStore();
  const { soundEnabled, setSoundEnabled } = useAudioStore();
  const { fetchAll } = useFriendStore();
  const { hasUnreadNotice, markNoticesAsRead } = useChatStore();
  const [connStatus, setConnStatus] = useState<ConnectionStatus>(wsClient.getStatus());

  useEffect(() => {
    fetchAll();

    const unsub = wsClient.onStatus((status) => {
      setConnStatus(status);
      if (status === 'connected') {
        fetchAll();
      }
    });
    return () => unsub();
  }, [fetchAll]);

  const isAdmin = role === 'admin' || role === 'superadmin' || username === 'superadmin';

  const handleOpenNoticeCenter = () => {
    markNoticesAsRead();
    onOpenNoticeDrawer?.();
  };

  const getStatusBadge = () => {
    switch (connStatus) {
      case 'connected':
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-100/80 text-emerald-800 border border-emerald-300/60">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            Protobuf 在线
          </span>
        );
      case 'connecting':
      case 'reconnecting':
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-amber-100/80 text-amber-800 border border-amber-300/60">
            <span className="w-2 h-2 rounded-full bg-amber-500 animate-ping" />
            连接中...
          </span>
        );
      default:
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-rose-100/80 text-rose-800 border border-rose-300/60">
            <span className="w-2 h-2 rounded-full bg-rose-500" />
            已断开
          </span>
        );
    }
  };

  return (
    <div className="flex flex-col h-[100dvh] w-screen overflow-hidden bg-[#FAF8F5] text-[#4A3B2C] pt-[env(safe-area-inset-top,0px)]">
      {/* 顶部柔光磨砂导航栏 */}
      <header className="h-14 sm:h-16 px-3 sm:px-6 flex items-center justify-between glass-panel border-b border-[#C9B99A]/30 z-30 shrink-0 select-none">
        {/* 左侧 Logo */}
        <div className="flex items-center gap-2 sm:gap-3">
          <div className="w-8 h-8 sm:w-10 sm:h-10 rounded-xl bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] flex items-center justify-center text-[#FAF8F5] shadow-walnut-glow shrink-0">
            <Sparkles size={16} className="sm:w-5 sm:h-5" />
          </div>
          <div>
            <div className="flex items-center gap-1.5 sm:gap-2">
              <h1 className="text-sm sm:text-base font-bold tracking-tight text-[#2E2419]">
                Neo IM
              </h1>
              <span className="text-[9px] sm:text-[10px] px-1.5 py-0.2 sm:py-0.5 rounded-md font-mono bg-[#8B7355]/15 text-[#8B7355] font-semibold">
                v2.0
              </span>
            </div>
            <p className="text-[10px] sm:text-[11px] text-[#A3927C] leading-none hidden sm:block">
              Warm Sand · 细腻暖沙与暖阳
            </p>
          </div>
        </div>

        {/* 中间状态指标 (小屏隐藏) */}
        <div className="hidden lg:flex items-center gap-3">
          {getStatusBadge()}
        </div>

        {/* 右侧功能控制 */}
        <div className="flex items-center gap-1.5 sm:gap-2.5">
          {/* 管理员专属运维监控入口 */}
          {isAdmin && (
            <motion.button
              whileTap={{ scale: 0.9 }}
              onClick={onOpenAdminDashboard}
              className="flex items-center gap-1 p-1.5 sm:px-3 sm:py-1.5 rounded-xl bg-gradient-to-r from-amber-600 to-[#8B7355] text-white text-xs font-semibold shadow-warm-sm hover:opacity-95 transition-all"
              title="运维监控中心"
            >
              <Shield size={14} />
              <span className="hidden sm:inline">看板</span>
            </motion.button>
          )}

          {/* 页面切换 Tab (聊天 / 动态广场) */}
          <div className="flex items-center p-0.5 sm:p-1 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40">
            <button
              onClick={() => setActiveView('chat')}
              className={`px-2.5 sm:px-3 py-1 text-xs font-semibold rounded-lg transition-all ${
                activeView === 'chat'
                  ? 'bg-[#8B7355] text-[#FAF8F5] shadow-warm-sm'
                  : 'text-[#8B7355] hover:text-[#2E2419]'
              }`}
            >
              聊天
            </button>
            <button
              onClick={() => setActiveView('profile')}
              className={`px-2.5 sm:px-3 py-1 text-xs font-semibold rounded-lg transition-all ${
                activeView === 'profile'
                  ? 'bg-[#8B7355] text-[#FAF8F5] shadow-warm-sm'
                  : 'text-[#8B7355] hover:text-[#2E2419]'
              }`}
            >
              广场
            </button>
          </div>

          {/* 系统通知公告小铃铛 */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={handleOpenNoticeCenter}
            title="系统公告历史与通知中心"
            className="relative p-1.5 sm:p-2 rounded-xl border bg-[#F0EBE3] border-[#C9B99A]/50 text-[#8B7355] hover:bg-[#EAE4DC] transition-colors shrink-0"
          >
            <Bell size={16} className="sm:w-[18px] sm:h-[18px]" />
            {hasUnreadNotice && (
              <span className="absolute -top-1 -right-1 w-2.5 h-2.5 rounded-full bg-amber-500 border-2 border-[#FAF8F5] animate-pulse" />
            )}
          </motion.button>

          {/* 音效开关 (小屏手机隐藏) */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={() => setSoundEnabled(!soundEnabled)}
            title={soundEnabled ? '音效已开启' : '音效已静音'}
            className={`hidden md:flex p-2 rounded-xl border transition-all shrink-0 ${
              soundEnabled
                ? 'bg-[#F0EBE3] border-[#C9B99A]/50 text-[#8B7355]'
                : 'bg-stone-200/50 border-stone-300 text-stone-400'
            }`}
          >
            {soundEnabled ? <Volume2 size={18} /> : <VolumeX size={18} />}
          </motion.button>

          {/* 用户身份与头像 */}
          <div className="flex items-center gap-1.5 pl-1.5 sm:pl-2 border-l border-[#C9B99A]/30 shrink-0">
            <div className="w-7 h-7 sm:w-8 sm:h-8 rounded-full bg-gradient-to-br from-[#C9B99A] to-[#8B7355] flex items-center justify-center text-[#FAF8F5] font-bold text-xs shadow-warm-sm overflow-hidden shrink-0">
              {avatarUrl ? (
                <img src={formatMediaUrl(avatarUrl)} alt="头像" className="w-full h-full object-cover" />
              ) : (
                username.slice(0, 1).toUpperCase()
              )}
            </div>
            <div className="hidden sm:flex items-center gap-1.5">
              <span className="text-xs font-semibold text-[#2E2419] max-w-[90px] truncate" title={`@${username}`}>
                {nickname || username}
              </span>
              <RoleBadge role={role} username={username} size="sm" />
            </div>
          </div>

          {/* 退出登录 */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={logout}
            title="退出登录"
            className="p-1.5 sm:p-2 rounded-xl text-[#8B7355] hover:bg-red-50 hover:text-red-600 transition-colors shrink-0"
          >
            <LogOut size={16} className="sm:w-[18px] sm:h-[18px]" />
          </motion.button>
        </div>
      </header>

      {/* 主体视窗容器 */}
      <main className="flex-1 flex overflow-hidden relative">
        {children}
      </main>
    </div>
  );
};
