import React, { useEffect, useState } from 'react';
import { useAuthStore, useAudioStore, useFriendStore } from '../../store';
import { wsClient, ConnectionStatus } from '../../socket/wsClient';
import {
  Sparkles,
  Volume2,
  VolumeX,
  User,
  LogOut,
  Radio,
  Share2,
} from 'lucide-react';
import { motion } from 'framer-motion';

interface AppLayoutProps {
  children: React.ReactNode;
  activeView: 'chat' | 'profile';
  setActiveView: (view: 'chat' | 'profile') => void;
}

export const AppLayout: React.FC<AppLayoutProps> = ({
  children,
  activeView,
  setActiveView,
}) => {
  const { username, logout } = useAuthStore();
  const { soundEnabled, setSoundEnabled } = useAudioStore();
  const { fetchAll } = useFriendStore();
  const [connStatus, setConnStatus] = useState<ConnectionStatus>(wsClient.getStatus());

  useEffect(() => {
    // 订阅 WebSocket 连接状态
    const unsub = wsClient.onStatus((status) => {
      setConnStatus(status);
      if (status === 'connected') {
        fetchAll();
      }
    });
    return () => unsub();
  }, [fetchAll]);

  const getStatusBadge = () => {
    switch (connStatus) {
      case 'connected':
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-100/80 text-emerald-800 border border-emerald-300/60">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            Protobuf 长连接在线
          </span>
        );
      case 'connecting':
      case 'reconnecting':
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-amber-100/80 text-amber-800 border border-amber-300/60">
            <span className="w-2 h-2 rounded-full bg-amber-500 animate-ping" />
            正在建立长连接...
          </span>
        );
      default:
        return (
          <span className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-rose-100/80 text-rose-800 border border-rose-300/60">
            <span className="w-2 h-2 rounded-full bg-rose-500" />
            连接已断开
          </span>
        );
    }
  };

  return (
    <div className="flex flex-col h-screen w-screen overflow-hidden bg-[#FAF8F5] text-[#4A3B2C]">
      {/* 顶部柔光磨砂导航栏 */}
      <header className="h-16 px-6 flex items-center justify-between glass-panel border-b border-[#C9B99A]/30 z-30 shrink-0">
        {/* 左侧 Logo */}
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] flex items-center justify-center text-[#FAF8F5] shadow-walnut-glow">
            <Sparkles size={20} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-bold tracking-tight text-[#2E2419]">
                Golang IM Neo
              </h1>
              <span className="text-[10px] px-1.5 py-0.5 rounded-md font-mono bg-[#8B7355]/15 text-[#8B7355] font-semibold">
                v2.0
              </span>
            </div>
            <p className="text-[11px] text-[#A3927C] leading-none">
              Warm Sand · 细腻暖沙与暖阳
            </p>
          </div>
        </div>

        {/* 中间状态指标 */}
        <div className="hidden md:flex items-center gap-3">
          {getStatusBadge()}
        </div>

        {/* 右侧功能控制 */}
        <div className="flex items-center gap-2.5">
          {/* 页面切换 Tab (聊天 / 动态广场) */}
          <div className="flex items-center p-1 rounded-xl bg-[#F0EBE3] border border-[#C9B99A]/40 mr-2">
            <button
              onClick={() => setActiveView('chat')}
              className={`px-3 py-1 text-xs font-semibold rounded-lg transition-all ${
                activeView === 'chat'
                  ? 'bg-[#8B7355] text-[#FAF8F5] shadow-warm-sm'
                  : 'text-[#8B7355] hover:text-[#2E2419]'
              }`}
            >
              聊天视窗
            </button>
            <button
              onClick={() => setActiveView('profile')}
              className={`px-3 py-1 text-xs font-semibold rounded-lg transition-all ${
                activeView === 'profile'
                  ? 'bg-[#8B7355] text-[#FAF8F5] shadow-warm-sm'
                  : 'text-[#8B7355] hover:text-[#2E2419]'
              }`}
            >
              动态广场
            </button>
          </div>

          {/* 音效开关 */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={() => setSoundEnabled(!soundEnabled)}
            title={soundEnabled ? '音效已开启' : '音效已静音'}
            className={`p-2 rounded-xl border transition-all ${
              soundEnabled
                ? 'bg-[#F0EBE3] border-[#C9B99A]/50 text-[#8B7355]'
                : 'bg-stone-200/50 border-stone-300 text-stone-400'
            }`}
          >
            {soundEnabled ? <Volume2 size={18} /> : <VolumeX size={18} />}
          </motion.button>

          {/* 用户身份与主页入口 */}
          <div className="flex items-center gap-2 pl-2 border-l border-[#C9B99A]/30">
            <div className="w-8 h-8 rounded-full bg-gradient-to-br from-[#C9B99A] to-[#8B7355] flex items-center justify-center text-[#FAF8F5] font-bold text-xs shadow-warm-sm">
              {username.slice(0, 1).toUpperCase()}
            </div>
            <span className="text-xs font-semibold text-[#2E2419] hidden sm:inline-block max-w-[100px] truncate">
              {username}
            </span>
          </div>

          {/* 退出登录 */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={logout}
            title="退出登录"
            className="p-2 rounded-xl text-[#8B7355] hover:bg-red-50 hover:text-red-600 transition-colors ml-1"
          >
            <LogOut size={18} />
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
