import React, { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { friendApi } from '../../api';
import { UserPlus, X, Loader2, CheckCircle2, AlertCircle } from 'lucide-react';

interface AddFriendModalProps {
  isOpen: boolean;
  onClose: () => void;
  initialUsername?: string;
}

export const AddFriendModal: React.FC<AddFriendModalProps> = ({
  isOpen,
  onClose,
  initialUsername = '',
}) => {
  const [targetUsername, setTargetUsername] = useState(initialUsername);
  const [remark, setRemark] = useState('');
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<{ type: 'success' | 'error'; msg: string } | null>(null);

  useEffect(() => {
    if (initialUsername) {
      setTargetUsername(initialUsername);
    }
  }, [initialUsername]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!targetUsername.trim()) return;

    setLoading(true);
    setStatus(null);

    try {
      const res = await friendApi.apply(targetUsername.trim(), remark.trim());
      if (res.code === 0) {
        setStatus({ type: 'success', msg: '好友申请已成功发送！等待对方审批' });
        setTimeout(() => {
          onClose();
          setTargetUsername('');
          setRemark('');
          setStatus(null);
        }, 1500);
      } else {
        setStatus({ type: 'error', msg: res.msg || '发送好友申请失败' });
      }
    } catch (err: any) {
      setStatus({ type: 'error', msg: err.message || '网络异常，请重试' });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.92, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.92, y: 15 }}
        className="relative w-full max-w-md p-6 rounded-3xl glass-card shadow-warm-lg"
      >
        <button
          onClick={onClose}
          className="absolute top-5 right-5 p-1.5 rounded-full text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
        >
          <X size={18} />
        </button>

        <div className="flex items-center gap-3 mb-5">
          <div className="w-10 h-10 rounded-2xl bg-[#8B7355] flex items-center justify-center text-white shadow-warm-sm">
            <UserPlus size={20} />
          </div>
          <div>
            <h3 className="font-bold text-base text-[#2E2419]">添加新好友</h3>
            <p className="text-xs text-[#A3927C]">通过用户名发起双向好友申请</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3.5">
          <div>
            <label className="block mb-1 text-xs font-semibold text-[#8B7355]">
              目标用户名
            </label>
            <input
              type="text"
              value={targetUsername}
              onChange={(e) => setTargetUsername(e.target.value)}
              placeholder="请输入对方的精准用户名"
              className="w-full px-3.5 py-2 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none"
              disabled={loading}
              autoFocus
            />
          </div>

          <div>
            <label className="block mb-1 text-xs font-semibold text-[#8B7355]">
              申请附言 (可选)
            </label>
            <input
              type="text"
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              placeholder="例如: 我是 Alice，交个朋友"
              className="w-full px-3.5 py-2 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none"
              disabled={loading}
            />
          </div>

          {status && (
            <div
              className={`p-2.5 rounded-xl text-xs flex items-center gap-2 ${
                status.type === 'success'
                  ? 'bg-emerald-50 text-emerald-800 border border-emerald-200'
                  : 'bg-red-50 text-red-800 border border-red-200'
              }`}
            >
              {status.type === 'success' ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
              <span>{status.msg}</span>
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl text-xs font-semibold text-[#8B7355] hover:bg-[#F0EBE3]"
            >
              取消
            </button>
            <motion.button
              whileTap={{ scale: 0.95 }}
              type="submit"
              disabled={loading || !targetUsername.trim()}
              className="px-5 py-2 rounded-xl bg-[#8B7355] hover:bg-[#7A6348] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-50"
            >
              {loading && <Loader2 size={14} className="animate-spin" />}
              <span>发送申请</span>
            </motion.button>
          </div>
        </form>
      </motion.div>
    </div>
  );
};
