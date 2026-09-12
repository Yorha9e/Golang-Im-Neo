import React, { useState } from 'react';
import { motion } from 'framer-motion';
import { groupApi } from '../../api';
import { useFriendStore } from '../../store';
import { PlusCircle, X, Loader2, CheckCircle2, AlertCircle } from 'lucide-react';

interface CreateGroupModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const CreateGroupModal: React.FC<CreateGroupModalProps> = ({ isOpen, onClose }) => {
  const [name, setName] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<{ type: 'success' | 'error'; msg: string } | null>(null);

  const { fetchGroups } = useFriendStore();

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    setLoading(true);
    setStatus(null);

    try {
      const res = await groupApi.createGroup({
        name: name.trim(),
        notice: notice.trim() || '欢迎加入群聊！',
      });
      if (res.code === 0) {
        setStatus({ type: 'success', msg: '群聊创建成功！' });
        await fetchGroups();
        setTimeout(() => {
          onClose();
          setName('');
          setNotice('');
          setStatus(null);
        }, 1200);
      } else {
        setStatus({ type: 'error', msg: res.msg || '创建群聊失败' });
      }
    } catch (err: any) {
      setStatus({ type: 'error', msg: err.message || '网络异常，请重试' });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
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
            <PlusCircle size={20} />
          </div>
          <div>
            <h3 className="font-bold text-base text-[#2E2419]">新建群聊</h3>
            <p className="text-xs text-[#A3927C]">创建多人协作与多对多即时频道</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3.5">
          <div>
            <label className="block mb-1 text-xs font-semibold text-[#8B7355]">
              群聊名称
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如: 极客前端研发群"
              className="w-full px-3.5 py-2 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none"
              disabled={loading}
              autoFocus
            />
          </div>

          <div>
            <label className="block mb-1 text-xs font-semibold text-[#8B7355]">
              群公告与说明 (可选)
            </label>
            <textarea
              value={notice}
              onChange={(e) => setNotice(e.target.value)}
              placeholder="输入群公告或规约..."
              rows={2}
              className="w-full px-3.5 py-2 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none resize-none"
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
              disabled={loading || !name.trim()}
              className="px-5 py-2 rounded-xl bg-[#8B7355] hover:bg-[#7A6348] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-50"
            >
              {loading && <Loader2 size={14} className="animate-spin" />}
              <span>立即创建</span>
            </motion.button>
          </div>
        </form>
      </motion.div>
    </div>
  );
};
