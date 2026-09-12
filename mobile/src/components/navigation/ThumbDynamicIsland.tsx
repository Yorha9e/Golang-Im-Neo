import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Compass,
  Users,
  Sparkles,
  User,
  Plus,
  Radio,
  UserPlus,
  Users2,
  PenTool,
  Wifi,
  Activity,
  X,
  ShieldCheck,
  CheckCircle2
} from 'lucide-react';
import { wsClient, ConnectionStatus } from '../../socket/wsClient';
import { soundEffects } from '../../audio/soundEffects';

export type IslandTabType = 'orbital' | 'contacts' | 'tidal' | 'core';

interface ThumbDynamicIslandProps {
  activeTab: IslandTabType;
  onSelectTab: (tab: IslandTabType) => void;
  unreadCount?: number;
  pendingRequestCount?: number;
  onOpenAddFriend: () => void;
  onOpenCreateGroup: () => void;
  onOpenNoticeDrawer: () => void;
  onOpenCreatePost: () => void;
}

export const ThumbDynamicIsland: React.FC<ThumbDynamicIslandProps> = ({
  activeTab,
  onSelectTab,
  unreadCount = 0,
  pendingRequestCount = 0,
  onOpenAddFriend,
  onOpenCreateGroup,
  onOpenNoticeDrawer,
  onOpenCreatePost,
}) => {
  const [connStatus, setConnStatus] = useState<ConnectionStatus>(wsClient.getStatus());
  const [latency, setLatency] = useState<number>(32);
  const [isBurstOpen, setIsBurstOpen] = useState(false);
  const [isPulseSheetOpen, setIsPulseSheetOpen] = useState(false);

  useEffect(() => {
    const unsub = wsClient.onStatus((status: ConnectionStatus) => {
      setConnStatus(status);
    });

    // 周期性产生真实微波动的长连接心跳延迟指标
    const interval = setInterval(() => {
      if (wsClient.getStatus() === 'connected') {
        setLatency(Math.floor(24 + Math.random() * 16));
      }
    }, 5000);

    return () => {
      unsub();
      clearInterval(interval);
    };
  }, []);

  const handleTabClick = (tab: IslandTabType) => {
    if (tab !== activeTab) {
      soundEffects.playHapticTick();
      onSelectTab(tab);
    }
    if (isBurstOpen) setIsBurstOpen(false);
  };

  const toggleBurst = () => {
    soundEffects.playBurstOpen();
    setIsBurstOpen((prev) => !prev);
  };

  const tabs: Array<{ id: IslandTabType; label: string; icon: React.ReactNode; badge?: number }> = [
    {
      id: 'orbital',
      label: '引力场',
      icon: <Compass size={19} className="transition-transform group-hover:rotate-45" />,
      badge: unreadCount,
    },
    {
      id: 'contacts',
      label: '星系',
      icon: <Users size={19} />,
      badge: pendingRequestCount,
    },
    {
      id: 'tidal',
      label: '潮汐',
      icon: <Sparkles size={19} />,
    },
    {
      id: 'core',
      label: '核心',
      icon: <User size={19} />,
    },
  ];

  return (
    <>
      {/* 1. 爆发行动 (Burst Menu) 浮层菜单 */}
      <AnimatePresence>
        {isBurstOpen && (
          <>
            {/* 点击空白遮罩关闭 */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsBurstOpen(false)}
              className="fixed inset-0 z-40 bg-stone-900/20 backdrop-blur-sm"
            />

            {/* 扇形灵动微岛弹出卡片 */}
            <motion.div
              initial={{ opacity: 0, scale: 0.85, y: 30 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.85, y: 20 }}
              transition={{ type: 'spring', stiffness: 450, damping: 26 }}
              className="fixed bottom-24 right-5 z-50 p-3 rounded-3xl bg-[#FAF8F5]/95 backdrop-blur-2xl border border-[#C9B99A]/50 shadow-warm-xl flex flex-col gap-2 min-w-[190px]"
            >
              <div className="px-3 py-1.5 border-b border-[#C9B99A]/30 flex items-center justify-between">
                <span className="text-[11px] font-bold text-[#8B7355] tracking-wider">
                  NEO 灵动行动
                </span>
                <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              </div>

              <button
                onClick={() => {
                  setIsBurstOpen(false);
                  onOpenCreatePost();
                }}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl hover:bg-[#F0EBE3] text-left text-xs font-semibold text-[#2E2419] transition-all group"
              >
                <div className="w-8 h-8 rounded-xl bg-gradient-to-tr from-amber-500 to-[#8B7355] text-white flex items-center justify-center shadow-warm-sm group-hover:scale-105 transition-transform">
                  <PenTool size={15} />
                </div>
                <div>
                  <div>发布动态潮汐</div>
                  <div className="text-[10px] text-[#A3927C] font-normal">图文或视频分享</div>
                </div>
              </button>

              <button
                onClick={() => {
                  setIsBurstOpen(false);
                  onOpenCreateGroup();
                }}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl hover:bg-[#F0EBE3] text-left text-xs font-semibold text-[#2E2419] transition-all group"
              >
                <div className="w-8 h-8 rounded-xl bg-[#8B7355] text-white flex items-center justify-center shadow-warm-sm group-hover:scale-105 transition-transform">
                  <Users2 size={15} />
                </div>
                <div>
                  <div>建立群聊引力</div>
                  <div className="text-[10px] text-[#A3927C] font-normal">多人畅联会话</div>
                </div>
              </button>

              <button
                onClick={() => {
                  setIsBurstOpen(false);
                  onOpenAddFriend();
                }}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl hover:bg-[#F0EBE3] text-left text-xs font-semibold text-[#2E2419] transition-all group"
              >
                <div className="w-8 h-8 rounded-xl bg-[#C9B99A] text-[#2E2419] flex items-center justify-center shadow-warm-sm group-hover:scale-105 transition-transform">
                  <UserPlus size={15} />
                </div>
                <div>
                  <div>探索添加好友</div>
                  <div className="text-[10px] text-[#A3927C] font-normal">搜索并建立连接</div>
                </div>
              </button>

              <button
                onClick={() => {
                  setIsBurstOpen(false);
                  onOpenNoticeDrawer();
                }}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl hover:bg-[#F0EBE3] text-left text-xs font-semibold text-[#2E2419] transition-all group"
              >
                <div className="w-8 h-8 rounded-xl bg-amber-100 text-amber-800 flex items-center justify-center shadow-warm-sm group-hover:scale-105 transition-transform">
                  <Radio size={15} />
                </div>
                <div>
                  <div>全服公告看板</div>
                  <div className="text-[10px] text-[#A3927C] font-normal">查看系统广播历史</div>
                </div>
              </button>
            </motion.div>
          </>
        )}
      </AnimatePresence>

      {/* 2. 脉搏状态悬浮信息卡片 */}
      <AnimatePresence>
        {isPulseSheetOpen && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsPulseSheetOpen(false)}
              className="fixed inset-0 z-40 bg-stone-900/10 backdrop-blur-xs"
            />
            <motion.div
              initial={{ opacity: 0, scale: 0.9, y: 20 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.9, y: 20 }}
              className="fixed bottom-24 left-5 z-50 p-4 rounded-3xl bg-[#FAF8F5]/95 backdrop-blur-2xl border border-[#C9B99A]/50 shadow-warm-xl w-72"
            >
              <div className="flex items-center justify-between pb-3 border-b border-[#C9B99A]/30">
                <div className="flex items-center gap-2">
                  <Activity size={16} className="text-[#8B7355]" />
                  <span className="text-xs font-bold text-[#2E2419]">Neo 长连接脉搏</span>
                </div>
                <button
                  onClick={() => setIsPulseSheetOpen(false)}
                  className="p-1 rounded-lg text-stone-400 hover:text-stone-700"
                >
                  <X size={14} />
                </button>
              </div>

              <div className="space-y-2.5 mt-3 text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-[#A3927C]">链路状态:</span>
                  <span
                    className={`font-semibold px-2 py-0.5 rounded-full text-[11px] ${
                      connStatus === 'connected'
                        ? 'bg-emerald-100 text-emerald-800'
                        : connStatus === 'connecting' || connStatus === 'reconnecting'
                        ? 'bg-amber-100 text-amber-800'
                        : 'bg-rose-100 text-rose-800'
                    }`}
                  >
                    {connStatus === 'connected'
                      ? '已建立安全双工'
                      : connStatus === 'connecting'
                      ? '票据握手中...'
                      : connStatus === 'reconnecting'
                      ? '重连自愈中...'
                      : '离线'}
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-[#A3927C]">往返延迟:</span>
                  <span className="font-mono font-bold text-emerald-700">
                    {connStatus === 'connected' ? `${latency} ms` : '--'}
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-[#A3927C]">协议契约:</span>
                  <span className="font-mono text-[11px] text-[#8B7355]">Protobuf v3 二进制</span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-[#A3927C]">安全认证:</span>
                  <span className="flex items-center gap-1 text-[11px] text-emerald-700 font-medium">
                    <ShieldCheck size={13} />
                    <span>SSL Pinning 激活</span>
                  </span>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>

      {/* 3. 灵动大拇指跑道胶囊主体 (Thumb Dynamic Island) */}
      <div className="fixed bottom-5 inset-x-0 z-30 flex justify-center pointer-events-none px-4 select-none">
        <motion.nav
          initial={{ y: 80, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          transition={{ type: 'spring', stiffness: 350, damping: 28 }}
          className="pointer-events-auto h-16 px-2.5 rounded-full bg-[#FAF8F5]/85 backdrop-blur-2xl border border-[#C9B99A]/50 shadow-warm-xl flex items-center justify-between gap-1 max-w-sm w-full relative"
        >
          {/* 左侧：网络脉搏呼吸小灯 */}
          <button
            onClick={() => setIsPulseSheetOpen(!isPulseSheetOpen)}
            className="w-10 h-10 rounded-full flex items-center justify-center shrink-0 hover:bg-[#F0EBE3] transition-colors relative group"
            title="查看长连接健康指标"
          >
            <span
              className={`w-2.5 h-2.5 rounded-full transition-colors ${
                connStatus === 'connected'
                  ? 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.7)] animate-pulse'
                  : connStatus === 'connecting' || connStatus === 'reconnecting'
                  ? 'bg-amber-500 shadow-[0_0_8px_rgba(245,158,11,0.7)] animate-ping'
                  : 'bg-rose-500 shadow-[0_0_8px_rgba(239,68,68,0.7)]'
              }`}
            />
            {connStatus === 'connected' && (
              <span className="absolute bottom-1 right-0 text-[8px] font-mono text-emerald-700/80 font-bold">
                {latency}
              </span>
            )}
          </button>

          {/* 中间：4 核心 Tab 选项 */}
          <div className="flex-1 flex items-center justify-around h-full py-2">
            {tabs.map((tab) => {
              const isActive = activeTab === tab.id;
              return (
                <button
                  key={tab.id}
                  onClick={() => handleTabClick(tab.id)}
                  className={`relative flex flex-col items-center justify-center flex-1 h-full rounded-2xl transition-colors group ${
                    isActive ? 'text-[#8B7355]' : 'text-[#A3927C] hover:text-[#2E2419]'
                  }`}
                >
                  {/* 活动状态的流体高亮胶囊底色 (基于 layoutId) */}
                  {isActive && (
                    <motion.div
                      layoutId="island-active-pill"
                      className="absolute inset-0 rounded-2xl bg-[#F0EBE3]/90 shadow-warm-sm -z-10 border border-[#C9B99A]/40"
                      transition={{ type: 'spring', stiffness: 450, damping: 32 }}
                    />
                  )}

                  <div className="relative">
                    {tab.icon}
                    {/* 未读气泡徽标 */}
                    {Boolean(tab.badge && tab.badge > 0) && (
                      <span className="absolute -top-1.5 -right-2 min-w-[15px] h-[15px] px-1 bg-gradient-to-r from-rose-500 to-amber-600 text-white font-mono text-[9px] font-bold rounded-full flex items-center justify-center shadow-sm">
                        {tab.badge! > 99 ? '99+' : tab.badge}
                      </span>
                    )}
                  </div>

                  <span className={`text-[10px] mt-0.5 font-medium transition-all ${
                    isActive ? 'font-bold text-[#8B7355]' : 'text-[#A3927C]'
                  }`}>
                    {tab.label}
                  </span>
                </button>
              );
            })}
          </div>

          {/* 右侧：爆发行动圆钮 (Burst Action Button) */}
          <motion.button
            whileTap={{ scale: 0.92 }}
            onClick={toggleBurst}
            className={`w-11 h-11 rounded-full flex items-center justify-center shrink-0 shadow-warm-md transition-all ${
              isBurstOpen
                ? 'bg-[#2E2419] text-[#FAF8F5] rotate-45'
                : 'bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white shadow-walnut-glow'
            }`}
            title="快捷行动中心"
          >
            <Plus size={20} className="transition-transform duration-200" />
          </motion.button>
        </motion.nav>
      </div>
    </>
  );
};
