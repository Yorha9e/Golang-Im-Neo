import React from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useAudioStore } from '../../store';
import { webRtcEngine } from '../../socket/webrtc';
import { Phone, PhoneOff, Sparkles } from 'lucide-react';

export const IncomingCallModal: React.FC = () => {
  const { incomingCall } = useAudioStore();

  if (!incomingCall || !incomingCall.isOpen) return null;

  const handleAccept = () => {
    webRtcEngine.acceptIncomingCall();
  };

  const handleReject = () => {
    webRtcEngine.rejectIncomingCall();
  };

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
        <motion.div
          initial={{ opacity: 0, scale: 0.88, y: 25 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.88, y: 25 }}
          transition={{ type: 'spring', stiffness: 350, damping: 25 }}
          className="relative w-full max-w-xs p-6 rounded-3xl glass-card border border-[#C9B99A]/50 shadow-warm-lg flex flex-col items-center text-center select-none"
        >
          {/* 来电呼吸声波光晕 */}
          <div className="relative my-4 flex items-center justify-center">
            <motion.div
              animate={{
                scale: [1, 1.35, 1.5],
                opacity: [0.7, 0.3, 0],
              }}
              transition={{
                repeat: Infinity,
                duration: 1.5,
                ease: 'easeOut',
              }}
              className="absolute w-20 h-20 rounded-full bg-emerald-400/40 -z-10"
            />
            <div className="w-16 h-16 rounded-full bg-gradient-to-tr from-[#8B7355] to-[#C9B99A] text-white flex items-center justify-center text-xl font-bold shadow-walnut-glow">
              {incomingCall.fromUsername.slice(0, 1).toUpperCase()}
            </div>
          </div>

          <h3 className="font-bold text-base text-[#2E2419]">
            {incomingCall.fromUsername}
          </h3>
          <p className="text-xs text-[#8B7355] mt-1 flex items-center gap-1.5 font-medium">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-ping" />
            <span>邀请你进行实时语音通话...</span>
          </p>

          {/* 接听与拒绝按钮 */}
          <div className="flex items-center justify-center gap-8 mt-8 w-full">
            {/* 拒绝 */}
            <div className="flex flex-col items-center gap-1.5">
              <motion.button
                whileTap={{ scale: 0.88 }}
                onClick={handleReject}
                className="w-12 h-12 rounded-2xl bg-rose-500 hover:bg-rose-600 text-white flex items-center justify-center shadow-lg shadow-rose-500/30 transition-all"
                title="拒绝通话"
              >
                <PhoneOff size={20} />
              </motion.button>
              <span className="text-[11px] text-[#A3927C]">拒绝</span>
            </div>

            {/* 接听 */}
            <div className="flex flex-col items-center gap-1.5">
              <motion.button
                whileTap={{ scale: 0.88 }}
                whileHover={{ scale: 1.08 }}
                onClick={handleAccept}
                className="w-14 h-14 rounded-2xl bg-emerald-500 hover:bg-emerald-600 text-white flex items-center justify-center shadow-lg shadow-emerald-500/40 transition-all animate-bounce"
                title="接听通话"
              >
                <Phone size={24} className="ml-0.5" />
              </motion.button>
              <span className="text-[11px] text-emerald-800 font-semibold">接听</span>
            </div>
          </div>
        </motion.div>
      </div>
    </AnimatePresence>
  );
};
