import React from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useChatStore } from '../../store';
import { Radio, X, Trash2, Bell, Sparkles } from 'lucide-react';

interface SystemNoticeDrawerProps {
  isOpen: boolean;
  onClose: () => void;
}

export const SystemNoticeDrawer: React.FC<SystemNoticeDrawerProps> = ({ isOpen, onClose }) => {
  const { systemNotices, clearSystemNotices } = useChatStore();

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-end bg-black/40 backdrop-blur-sm select-none">
      <motion.div
        initial={{ opacity: 0, x: 300 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 300 }}
        transition={{ type: 'spring', stiffness: 350, damping: 28 }}
        className="w-full max-w-sm h-full glass-card border-l border-[#C9B99A]/40 shadow-2xl p-6 flex flex-col"
      >
        {/* 抽屉头部 */}
        <div className="flex items-center justify-between pb-4 border-b border-[#C9B99A]/30">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-xl bg-[#8B7355] text-white flex items-center justify-center shadow-warm-sm">
              <Bell size={16} />
            </div>
            <div>
              <h3 className="font-bold text-sm text-[#2E2419]">系统公告中心</h3>
              <p className="text-[10px] text-[#A3927C]">全服广播与官方通知历史</p>
            </div>
          </div>

          <div className="flex items-center gap-1">
            {systemNotices.length > 0 && (
              <button
                onClick={clearSystemNotices}
                className="p-1.5 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-colors"
                title="清空历史通知"
              >
                <Trash2 size={15} />
              </button>
            )}
            <button
              onClick={onClose}
              className="p-1.5 rounded-lg text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
            >
              <X size={18} />
            </button>
          </div>
        </div>

        {/* 列表主体 */}
        <div className="flex-1 overflow-y-auto space-y-3 pt-4 pr-1">
          {systemNotices.length === 0 ? (
            <div className="py-24 text-center text-xs text-[#A3927C]">
              暂无历史系统公告
            </div>
          ) : (
            systemNotices.map((n) => (
              <div
                key={n.id}
                className="p-4 rounded-2xl glass-input space-y-1.5 border border-[#C9B99A]/40"
              >
                <div className="flex items-center justify-between text-[11px] text-[#8B7355] font-semibold">
                  <span className="flex items-center gap-1">
                    <Radio size={12} className="text-amber-600" />
                    <span>官方广播</span>
                  </span>
                  <span className="text-[10px] text-[#A3927C] font-mono">
                    {new Date(n.timestamp).toLocaleTimeString()}
                  </span>
                </div>
                <p className="text-xs text-[#2E2419] leading-relaxed select-text">
                  {n.content}
                </p>
              </div>
            ))
          )}
        </div>
      </motion.div>
    </div>
  );
};
