import React, { useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useChatStore } from '../../store';
import { Radio, X, Sparkles } from 'lucide-react';

export const GlobalBroadcastBanner: React.FC = () => {
  const { activeBroadcastNotice, dismissBroadcastNotice } = useChatStore();

  // 8秒后自动淡出
  useEffect(() => {
    if (activeBroadcastNotice) {
      const timer = setTimeout(() => {
        dismissBroadcastNotice();
      }, 8000);
      return () => clearTimeout(timer);
    }
  }, [activeBroadcastNotice, dismissBroadcastNotice]);

  if (!activeBroadcastNotice) return null;

  return (
    <AnimatePresence>
      <div className="fixed top-4 inset-x-0 z-50 flex justify-center px-4 pointer-events-none select-none">
        <motion.div
          initial={{ opacity: 0, y: -60, scale: 0.95 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: -60, scale: 0.95 }}
          transition={{ type: 'spring', stiffness: 400, damping: 26 }}
          className="pointer-events-auto relative max-w-xl w-full p-4 rounded-3xl bg-gradient-to-r from-[#8B7355] via-[#A38A6A] to-[#8B7355] text-white shadow-2xl border border-amber-300/40 flex items-center gap-3.5 overflow-hidden"
        >
          {/* 背景流光动画 (严格设置 pointer-events-none 防止阻挡点击事件) */}
          <div className="absolute inset-0 bg-gradient-to-r from-transparent via-white/10 to-transparent -skew-x-12 animate-pulse pointer-events-none" />

          {/* 喇叭发光图标 */}
          <div className="w-10 h-10 rounded-2xl bg-white/20 flex items-center justify-center shrink-0 shadow-inner pointer-events-none">
            <Radio size={20} className="text-amber-200 animate-bounce" />
          </div>

          <div className="flex-1 min-w-0 pointer-events-none">
            <div className="flex items-center gap-2">
              <span className="text-xs font-bold tracking-wide text-amber-200 flex items-center gap-1">
                <Sparkles size={12} />
                <span>全服系统官方公告</span>
              </span>
              <span className="text-[10px] text-white/70 font-mono">
                {new Date(activeBroadcastNotice.timestamp).toLocaleTimeString()}
              </span>
            </div>
            <p className="text-xs font-medium text-white/95 mt-0.5 leading-relaxed line-clamp-2 select-text pointer-events-auto">
              {activeBroadcastNotice.content}
            </p>
          </div>

          {/* 手动关闭按钮 (提升 z-index 并保证点击畅通) */}
          <motion.button
            whileTap={{ scale: 0.85 }}
            whileHover={{ scale: 1.1 }}
            onClick={(e) => {
              e.stopPropagation();
              dismissBroadcastNotice();
            }}
            className="relative z-20 p-2 rounded-full bg-white/15 hover:bg-white/30 text-white transition-colors shrink-0 cursor-pointer shadow-sm"
            title="关闭公告"
          >
            <X size={16} />
          </motion.button>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};
