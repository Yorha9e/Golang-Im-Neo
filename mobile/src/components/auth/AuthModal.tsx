import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useAuthStore } from '../../store';
import { Sparkles, Lock, User, ArrowRight, Loader2, ShieldCheck } from 'lucide-react';

export const AuthModal: React.FC = () => {
  const [isLogin, setIsLogin] = useState(true);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [errorMsg, setErrorMsg] = useState('');
  const [loading, setLoading] = useState(false);

  const { login, register } = useAuthStore();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg('');

    if (!username.trim()) {
      setErrorMsg('请输入用户名');
      return;
    }
    if (password.length < 8) {
      setErrorMsg('密码至少需要 8 位字符');
      return;
    }

    setLoading(true);
    try {
      if (isLogin) {
        await login({ username: username.trim(), password });
      } else {
        await register({ username: username.trim(), password });
      }
    } catch (err: any) {
      setErrorMsg(err.message || '操作失败，请重试');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/30 backdrop-blur-md">
      {/* 暖沙柔光流体背景光斑 */}
      <div className="absolute w-96 h-96 bg-[#C9B99A]/30 rounded-full blur-3xl pointer-events-none -top-12 -left-12 animate-pulse-subtle" />
      <div className="absolute w-96 h-96 bg-[#8B7355]/20 rounded-full blur-3xl pointer-events-none -bottom-12 -right-12" />

      <motion.div
        initial={{ opacity: 0, scale: 0.92, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.92, y: 15 }}
        transition={{ type: 'spring', stiffness: 350, damping: 28 }}
        className="relative w-full max-w-md p-8 overflow-hidden rounded-3xl glass-card shadow-warm-lg"
      >
        {/* 顶部 Brand 与标语 */}
        <div className="flex flex-col items-center mb-8 text-center">
          <motion.div
            whileHover={{ rotate: 15, scale: 1.08 }}
            className="w-14 h-14 mb-3 rounded-2xl bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] flex items-center justify-center text-[#FAF8F5] shadow-walnut-glow"
          >
            <Sparkles size={28} />
          </motion.div>
          <h2 className="text-2xl font-bold tracking-tight text-[#2E2419]">
            Golang IM Neo
          </h2>
          <p className="mt-1 text-sm text-[#8B7355]/80 font-medium">
            Warm Sand · 细腻暖沙与暖阳
          </p>
        </div>

        {/* 登录 / 注册 Tab 滑动切换 */}
        <div className="relative flex p-1 mb-6 rounded-2xl bg-[#F0EBE3] border border-[#C9B99A]/30">
          <button
            type="button"
            onClick={() => { setIsLogin(true); setErrorMsg(''); }}
            className={`relative flex-1 py-2 text-sm font-semibold transition-colors z-10 ${
              isLogin ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {isLogin && (
              <motion.div
                layoutId="authTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10">登录账户</span>
          </button>

          <button
            type="button"
            onClick={() => { setIsLogin(false); setErrorMsg(''); }}
            className={`relative flex-1 py-2 text-sm font-semibold transition-colors z-10 ${
              !isLogin ? 'text-[#FAF8F5]' : 'text-[#8B7355] hover:text-[#2E2419]'
            }`}
          >
            {!isLogin && (
              <motion.div
                layoutId="authTabIndicator"
                className="absolute inset-0 rounded-xl bg-[#8B7355] shadow-warm-sm"
                transition={{ type: 'spring', stiffness: 450, damping: 30 }}
              />
            )}
            <span className="relative z-10">注册新账号</span>
          </button>
        </div>

        {/* 表单区域 */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block mb-1.5 text-xs font-semibold text-[#8B7355] uppercase tracking-wider">
              用户名
            </label>
            <div className="relative flex items-center">
              <User className="absolute left-3.5 text-[#C9B99A]" size={18} />
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="例如: alice / bob"
                className="w-full pl-10 pr-4 py-2.5 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none transition-all"
                disabled={loading}
              />
            </div>
          </div>

          <div>
            <label className="block mb-1.5 text-xs font-semibold text-[#8B7355] uppercase tracking-wider">
              密码 (至少 8 位)
            </label>
            <div className="relative flex items-center">
              <Lock className="absolute left-3.5 text-[#C9B99A]" size={18} />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="w-full pl-10 pr-4 py-2.5 rounded-xl glass-input text-sm text-[#2E2419] placeholder-[#A3927C] outline-none transition-all"
                disabled={loading}
              />
            </div>
          </div>

          {/* 错误提示 */}
          <AnimatePresence mode="wait">
            {errorMsg && (
              <motion.div
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                exit={{ opacity: 0, height: 0 }}
                className="px-3 py-2 rounded-xl bg-red-50/90 border border-red-200 text-red-700 text-xs font-medium"
              >
                {errorMsg}
              </motion.div>
            )}
          </AnimatePresence>

          {/* 提交按钮 */}
          <motion.button
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
            type="submit"
            disabled={loading}
            className="w-full mt-2 py-3 px-4 rounded-xl bg-gradient-to-r from-[#8B7355] to-[#7A6348] hover:from-[#7A6348] hover:to-[#6B553D] text-[#FAF8F5] text-sm font-semibold flex items-center justify-center gap-2 shadow-walnut-glow transition-all disabled:opacity-60"
          >
            {loading ? (
              <>
                <Loader2 size={18} className="animate-spin" />
                <span>处理中...</span>
              </>
            ) : (
              <>
                <span>{isLogin ? '立即进入' : '创建并登录'}</span>
                <ArrowRight size={18} />
              </>
            )}
          </motion.button>
        </form>

        {/* 底部保障 */}
        <div className="flex items-center justify-center gap-1.5 mt-6 text-xs text-[#A3927C]">
          <ShieldCheck size={14} className="text-[#8B7355]" />
          <span>WebSocket 二进制 Protobuf 物理加密通道</span>
        </div>
      </motion.div>
    </div>
  );
};
