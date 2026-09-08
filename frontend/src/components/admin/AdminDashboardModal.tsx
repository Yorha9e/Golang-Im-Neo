import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { adminApi, SystemStats } from '../../api';
import {
  Shield,
  Activity,
  Users,
  Radio,
  HardDrive,
  Cpu,
  RefreshCw,
  Send,
  X,
  Lock,
  MessageSquare,
  AlertTriangle,
  Loader2,
  CheckCircle2,
} from 'lucide-react';

interface AdminDashboardModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const AdminDashboardModal: React.FC<AdminDashboardModalProps> = ({ isOpen, onClose }) => {
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [loading, setLoading] = useState(false);

  // 广播状态
  const [broadcastContent, setBroadcastContent] = useState('');
  const [sendingBroadcast, setSendingBroadcast] = useState(false);
  const [broadcastSuccess, setBroadcastSuccess] = useState(false);

  // 封禁状态
  const [banUserId, setBanUserId] = useState('');
  const [banning, setBanning] = useState(false);
  const [banSuccess, setBanSuccess] = useState(false);

  useEffect(() => {
    if (isOpen) {
      loadStats();
      // 开启 5s 自动轮询刷新
      const timer = setInterval(() => {
        loadStats(false);
      }, 5000);
      return () => clearInterval(timer);
    }
  }, [isOpen]);

  const loadStats = async (showLoading = true) => {
    if (showLoading) setLoading(true);
    try {
      const res = await adminApi.getStats();
      if (res.code === 0 && res.data) {
        setStats(res.data);
      }
    } catch (err) {
      console.error('拉取监控指标失败:', err);
    } finally {
      if (showLoading) setLoading(false);
    }
  };

  // 发送全服广播
  const handleBroadcast = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!broadcastContent.trim()) return;

    setSendingBroadcast(true);
    try {
      const res = await adminApi.broadcast(broadcastContent.trim());
      if (res.code === 0) {
        setBroadcastSuccess(true);
        setBroadcastContent('');
        setTimeout(() => setBroadcastSuccess(false), 2500);
        loadStats(false);
      } else {
        alert(res.msg || '发送广播失败');
      }
    } catch (err: any) {
      alert(err.message || '网络异常');
    } finally {
      setSendingBroadcast(false);
    }
  };

  // 封禁用户
  const handleBanUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!banUserId.trim()) return;
    if (!window.confirm(`确认封禁用户 ${banUserId.trim()}？此操作将立即掐断其全部在线会话！`)) return;

    setBanning(true);
    try {
      const res = await adminApi.banUser(banUserId.trim());
      if (res.code === 0) {
        setBanSuccess(true);
        setBanUserId('');
        setTimeout(() => setBanSuccess(false), 2500);
        loadStats(false);
      } else {
        alert(res.msg || '封禁失败');
      }
    } catch (err: any) {
      alert(err.message || '网络异常');
    } finally {
      setBanning(false);
    }
  };

  if (!isOpen) return null;

  const formatUptime = (secs: number = 0) => {
    const hours = Math.floor(secs / 3600);
    const mins = Math.floor((secs % 3600) / 60);
    const s = secs % 60;
    return `${hours}h ${mins}m ${s}s`;
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50 backdrop-blur-md select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.9, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.9, y: 20 }}
        transition={{ type: 'spring', stiffness: 350, damping: 26 }}
        className="relative w-full max-w-3xl p-6 rounded-3xl glass-card shadow-warm-lg max-h-[90vh] flex flex-col overflow-hidden"
      >
        {/* 顶部标题与刷新 */}
        <div className="flex items-center justify-between pb-4 border-b border-[#C9B99A]/30 shrink-0">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-2xl bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white flex items-center justify-center shadow-walnut-glow">
              <Shield size={22} />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="font-bold text-base text-[#2E2419]">
                  系统运维与全服监控中心
                </h3>
                <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-amber-100 text-amber-900 font-bold">
                  ADMIN
                </span>
              </div>
              <p className="text-xs text-[#A3927C] flex items-center gap-2 mt-0.5">
                <span>运行时间: {formatUptime(stats?.server?.uptime_seconds)}</span>
                <span>·</span>
                <span>Goroutines: {stats?.server?.num_goroutine || 0}</span>
                <span>·</span>
                <span>Go: {stats?.server?.go_version || '1.26'}</span>
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={() => loadStats(true)}
              className="p-2 rounded-xl text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
              title="手动刷新指标"
            >
              <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
            </button>
            <button
              onClick={onClose}
              className="p-2 rounded-xl text-[#8B7355] hover:bg-[#F0EBE3] transition-colors"
            >
              <X size={18} />
            </button>
          </div>
        </div>

        {/* 滚动内容区 */}
        <div className="flex-1 overflow-y-auto space-y-6 pt-5 pr-1">
          {/* 1. 四大核心性能指标看板 */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            {/* 在线长连接 */}
            <div className="p-3.5 rounded-2xl glass-input space-y-1">
              <div className="flex items-center justify-between text-xs text-[#8B7355]">
                <span className="font-semibold">在线长连接</span>
                <Radio size={14} className="text-emerald-600 animate-pulse" />
              </div>
              <div className="text-xl font-bold font-mono text-[#2E2419]">
                {stats?.gateway?.online_connections ?? '--'}
              </div>
              <p className="text-[10px] text-[#A3927C]">
                {stats?.gateway?.shard_count || 32} 分段锁并发池
              </p>
            </div>

            {/* 批量写入队列深度 */}
            <div className="p-3.5 rounded-2xl glass-input space-y-1">
              <div className="flex items-center justify-between text-xs text-[#8B7355]">
                <span className="font-semibold">入库削峰队列</span>
                <Activity size={14} className="text-[#8B7355]" />
              </div>
              <div className="text-xl font-bold font-mono text-[#2E2419]">
                {stats?.store?.batch_queue_len ?? '--'}
              </div>
              <p className="text-[10px] text-[#A3927C]">
                容量: {stats?.store?.batch_queue_cap || 10000}
              </p>
            </div>

            {/* 全服注册用户 */}
            <div className="p-3.5 rounded-2xl glass-input space-y-1">
              <div className="flex items-center justify-between text-xs text-[#8B7355]">
                <span className="font-semibold">注册总用户</span>
                <Users size={14} className="text-[#8B7355]" />
              </div>
              <div className="text-xl font-bold font-mono text-[#2E2419]">
                {stats?.counts?.users_total ?? '--'}
              </div>
              <p className="text-[10px] text-[#A3927C]">
                已封禁: {stats?.counts?.users_banned ?? 0} 人
              </p>
            </div>

            {/* 累计总消息量 */}
            <div className="p-3.5 rounded-2xl glass-input space-y-1">
              <div className="flex items-center justify-between text-xs text-[#8B7355]">
                <span className="font-semibold">全服消息总数</span>
                <MessageSquare size={14} className="text-[#8B7355]" />
              </div>
              <div className="text-xl font-bold font-mono text-[#2E2419]">
                {stats?.counts?.messages_total ?? '--'}
              </div>
              <p className="text-[10px] text-[#A3927C]">
                群聊总数: {stats?.counts?.groups_total ?? 0} 个
              </p>
            </div>
          </div>

          {/* 2. 全服系统广播发布器 */}
          <div className="p-5 rounded-3xl glass-card border border-[#C9B99A]/40 space-y-3">
            <div className="flex items-center justify-between">
              <h4 className="font-bold text-xs text-[#2E2419] flex items-center gap-1.5">
                <Radio size={14} className="text-[#8B7355]" />
                <span>发布全服系统公告广播</span>
              </h4>
              <span className="text-[10px] text-[#A3927C]">
                将以 SYSTEM_NOTICE 实时弹窗推向全服所有在线设备
              </span>
            </div>

            <form onSubmit={handleBroadcast} className="space-y-2.5">
              <textarea
                value={broadcastContent}
                onChange={(e) => setBroadcastContent(e.target.value)}
                placeholder="输入系统维护、升级提醒或官方公告内容..."
                rows={2}
                className="w-full p-3 rounded-2xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none resize-none select-text"
                disabled={sendingBroadcast}
              />

              <div className="flex items-center justify-between">
                {broadcastSuccess ? (
                  <span className="text-xs text-emerald-700 flex items-center gap-1 font-semibold">
                    <CheckCircle2 size={14} />
                    全服广播推送成功！
                  </span>
                ) : <span />}

                <motion.button
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.95 }}
                  type="submit"
                  disabled={sendingBroadcast || !broadcastContent.trim()}
                  className="px-4 py-1.5 rounded-xl bg-gradient-to-r from-[#8B7355] to-[#7A6348] text-white text-xs font-semibold flex items-center gap-1.5 shadow-walnut-glow disabled:opacity-50"
                >
                  {sendingBroadcast && <Loader2 size={13} className="animate-spin" />}
                  <span>立即广播</span>
                  <Send size={12} />
                </motion.button>
              </div>
            </form>
          </div>

          {/* 3. 违规用户治理与封禁 */}
          <div className="p-5 rounded-3xl glass-card border border-[#C9B99A]/40 space-y-3">
            <div className="flex items-center justify-between">
              <h4 className="font-bold text-xs text-[#2E2419] flex items-center gap-1.5">
                <Lock size={14} className="text-rose-600" />
                <span>违规用户治理 (精准封禁)</span>
              </h4>
              <span className="text-[10px] text-[#A3927C]">
                将递增 TokenVersion 并立即掐断长连接
              </span>
            </div>

            <form onSubmit={handleBanUser} className="flex gap-2">
              <input
                type="text"
                value={banUserId}
                onChange={(e) => setBanUserId(e.target.value)}
                placeholder="输入要封禁的目标 UserID (如: u_05cc5f23)"
                className="flex-1 px-3 py-2 rounded-xl glass-input text-xs text-[#2E2419] placeholder-[#A3927C] outline-none font-mono select-text"
                disabled={banning}
              />

              <motion.button
                whileTap={{ scale: 0.95 }}
                type="submit"
                disabled={banning || !banUserId.trim()}
                className="px-4 py-2 rounded-xl bg-rose-600 hover:bg-rose-700 text-white text-xs font-semibold flex items-center gap-1.5 shadow-md shadow-rose-600/30 disabled:opacity-50"
              >
                {banning && <Loader2 size={13} className="animate-spin" />}
                <span>封禁用户</span>
              </motion.button>
            </form>

            {banSuccess && (
              <p className="text-xs text-emerald-700 flex items-center gap-1 font-semibold pt-1">
                <CheckCircle2 size={14} />
                用户已成功封禁并强制踢下线！
              </p>
            )}
          </div>
        </div>
      </motion.div>
    </div>
  );
};
