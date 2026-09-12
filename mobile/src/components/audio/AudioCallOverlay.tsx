import React from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useAudioStore } from '../../store';
import { webRtcEngine } from '../../socket/webrtc';
import { PhoneOff, Mic, MicOff, AlertCircle } from 'lucide-react';

export const AudioCallOverlay: React.FC = () => {
  const { webRtcCall } = useAudioStore();
  const { isOpen, targetUsername, status, isMuted } = webRtcCall;

  if (!isOpen) return null;

  const handleToggleMute = () => {
    webRtcEngine.toggleMute(!isMuted);
  };

  const handleHangup = () => {
    webRtcEngine.hangup();
  };

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, scale: 0.85, y: -20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.85, y: -20 }}
        transition={{ type: 'spring', stiffness: 350, damping: 25 }}
        className="fixed top-20 right-8 z-50 p-5 rounded-3xl glass-card border border-[#C9B99A]/50 shadow-warm-lg flex flex-col items-center max-w-xs min-w-[270px] select-none"
      >
        {/* 声纹呼吸光晕外圈 (Warm Sand Wheat & Walnut 光环) */}
        <div className="relative my-3 flex items-center justify-center">
          {/* 扩散波纹 */}
          <motion.div
            animate={{
              scale: status === 'connected' ? [1, 1.4, 1.6] : [1, 1.2, 1],
              opacity: status === 'connected' ? [0.6, 0.3, 0] : [0.4, 0.1, 0],
            }}
            transition={{
              repeat: Infinity,
              duration: 2,
              ease: 'easeOut',
            }}
            className="absolute w-24 h-24 rounded-full bg-gradient-to-tr from-[#C9B99A] to-[#8B7355] -z-10"
          />

          <motion.div
            animate={{
              scale: status === 'connected' ? [1, 1.25, 1.4] : 1,
              opacity: status === 'connected' ? [0.8, 0.4, 0] : 0,
            }}
            transition={{
              repeat: Infinity,
              duration: 2,
              delay: 0.4,
              ease: 'easeOut',
            }}
            className="absolute w-20 h-20 rounded-full bg-[#C9B99A]/40 -z-10"
          />

          {/* 头像 */}
          <div className="w-16 h-16 rounded-full bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white flex items-center justify-center text-xl font-bold shadow-walnut-glow">
            {(targetUsername || 'User').slice(0, 1).toUpperCase()}
          </div>
        </div>

        {/* 通话状态文字 */}
        <h4 className="font-bold text-sm text-[#2E2419]">{targetUsername || '好友通话'}</h4>
        <p className="text-[11px] text-[#8B7355] mt-0.5 flex items-center gap-1 font-medium">
          {status === 'calling' ? (
            <>
              <span className="w-2 h-2 rounded-full bg-amber-500 animate-ping" />
              <span>正在建立端对端 WebRTC 连接...</span>
            </>
          ) : (
            <>
              <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              <span>P2P 实时高清语音通话中</span>
            </>
          )}
        </p>

        {/* 控制按键 */}
        <div className="flex items-center gap-4 mt-4">
          {/* 静音按钮 */}
          <motion.button
            whileTap={{ scale: 0.88 }}
            onClick={handleToggleMute}
            className={`w-11 h-11 rounded-2xl flex items-center justify-center transition-all shadow-warm-sm ${
              isMuted
                ? 'bg-rose-100 text-rose-700 border border-rose-300'
                : 'bg-[#F0EBE3] hover:bg-[#EAE4DC] text-[#8B7355] border border-[#C9B99A]/40'
            }`}
          >
            {isMuted ? <MicOff size={18} /> : <Mic size={18} />}
          </motion.button>

          {/* 挂断按钮 */}
          <motion.button
            whileTap={{ scale: 0.88 }}
            whileHover={{ scale: 1.05 }}
            onClick={handleHangup}
            className="w-11 h-11 rounded-2xl bg-rose-600 hover:bg-rose-700 text-white flex items-center justify-center shadow-lg shadow-rose-600/30 transition-all"
          >
            <PhoneOff size={18} />
          </motion.button>
        </div>

        {/* 浏览器环境与麦克风安全策略提示 */}
        <div className="mt-4 pt-3 border-t border-[#C9B99A]/30 flex items-start gap-1.5 text-[10px] text-[#A3927C] leading-tight text-left">
          <AlertCircle size={12} className="shrink-0 mt-0.5 text-[#8B7355]" />
          <span>
            提示：非 HTTPS 或部分移动浏览器受系统沙箱限制可能无法唤起麦克风，推荐在 Localhost、标准 HTTPS 或真机客户端下使用。
          </span>
        </div>
      </motion.div>
    </AnimatePresence>
  );
};
