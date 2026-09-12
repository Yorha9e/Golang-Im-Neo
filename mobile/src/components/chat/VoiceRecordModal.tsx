import React, { useEffect, useRef, useState } from 'react';
import { motion } from 'framer-motion';
import { useChatStore } from '../../store';
import { mediaApi } from '../../api';
import { soundEffects } from '../../audio/soundEffects';
import { Trash2, Send, Loader2, AlertCircle } from 'lucide-react';

interface VoiceRecordModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const VoiceRecordModal: React.FC<VoiceRecordModalProps> = ({ isOpen, onClose }) => {
  const [isRecording, setIsRecording] = useState(false);
  const [recordDuration, setRecordDuration] = useState(0);
  const [isUploading, setIsUploading] = useState(false);
  const [liveAmplitudes, setLiveAmplitudes] = useState<number[]>(new Array(16).fill(0.15));

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const audioChunksRef = useRef<Blob[]>([]);
  const audioContextRef = useRef<AudioContext | null>(null);
  const analyserRef = useRef<AnalyserNode | null>(null);
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const animationFrameRef = useRef<number | null>(null);
  const durationTimerRef = useRef<any>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);

  const { sendMediaMessage } = useChatStore();

  useEffect(() => {
    if (isOpen) {
      startRecordingProcess();
    } else {
      cleanupAudio();
    }
    return () => cleanupAudio();
  }, [isOpen]);

  const startRecordingProcess = async () => {
    try {
      soundEffects.playRecordTone(true);
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      mediaStreamRef.current = stream;

      // 创建 Web Audio 分析器以获得实时频谱振幅
      const AudioCtx = window.AudioContext || (window as any).webkitAudioContext;
      const audioCtx = new AudioCtx();
      audioContextRef.current = audioCtx;

      const analyser = audioCtx.createAnalyser();
      analyser.fftSize = 64;
      analyserRef.current = analyser;

      const source = audioCtx.createMediaStreamSource(stream);
      sourceRef.current = source;
      source.connect(analyser);

      // 启动 MediaRecorder
      const recorder = new MediaRecorder(stream);
      mediaRecorderRef.current = recorder;
      audioChunksRef.current = [];

      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) {
          audioChunksRef.current.push(e.data);
        }
      };

      recorder.start(100);
      setIsRecording(true);
      setRecordDuration(0);

      // 启动秒数计时器 (上限 60 秒)
      durationTimerRef.current = setInterval(() => {
        setRecordDuration((prev) => {
          if (prev >= 59) {
            handleStopAndSend();
            return 60;
          }
          return prev + 1;
        });
      }, 1000);

      // 实时分析振幅动画
      const updateVisualizer = () => {
        if (!analyserRef.current) return;
        const dataArray = new Uint8Array(analyserRef.current.frequencyBinCount);
        analyserRef.current.getByteFrequencyData(dataArray);

        // 取前 16 个频段做归一化
        const amps: number[] = [];
        for (let i = 0; i < 16; i++) {
          const val = (dataArray[i] || 0) / 255;
          amps.push(Math.max(0.15, val));
        }
        setLiveAmplitudes(amps);

        animationFrameRef.current = requestAnimationFrame(updateVisualizer);
      };
      updateVisualizer();
    } catch (err) {
      console.error('麦克风权限获取失败:', err);
      alert('无法访问麦克风：非 HTTPS 或部分移动浏览器受系统沙箱限制。建议在 Localhost 或桌面端体验。');
      onClose();
    }
  };

  const cleanupAudio = () => {
    if (animationFrameRef.current) {
      cancelAnimationFrame(animationFrameRef.current);
      animationFrameRef.current = null;
    }
    if (durationTimerRef.current) {
      clearInterval(durationTimerRef.current);
      durationTimerRef.current = null;
    }
    if (mediaRecorderRef.current && mediaRecorderRef.current.state !== 'inactive') {
      mediaRecorderRef.current.stop();
    }
    if (mediaStreamRef.current) {
      mediaStreamRef.current.getTracks().forEach((track) => track.stop());
      mediaStreamRef.current = null;
    }
    if (audioContextRef.current) {
      audioContextRef.current.close();
      audioContextRef.current = null;
    }
    setIsRecording(false);
  };

  // 取消录音
  const handleCancel = () => {
    soundEffects.playRecordTone(false);
    cleanupAudio();
    onClose();
  };

  // 停止并发送录音
  const handleStopAndSend = async () => {
    soundEffects.playRecordTone(false);
    if (!mediaRecorderRef.current) return;

    setIsUploading(true);

    mediaRecorderRef.current.onstop = async () => {
      const audioBlob = new Blob(audioChunksRef.current, { type: 'audio/mp3' });
      const duration = Math.max(1, recordDuration);

      // 生成 12 个固定特征 Peaks 供气泡波形渲染
      const peaks = liveAmplitudes.slice(0, 12).map((a) => Math.round(a * 100) / 100);

      try {
        // 使用后端白名单内的 .mp3 扩展名
        const res = await mediaApi.upload(audioBlob, 'voice', 'public', 'voice_message.mp3');
        if (res.code === 0 && res.data) {
          const mediaUrl = res.data.access_url || res.data.url;
          if (mediaUrl) {
            await sendMediaMessage(mediaUrl, 'voice', {
              mid: res.data.mid,
              duration,
              peaks,
            });
          }
        } else {
          alert(res.msg || '语音上传失败');
        }
      } catch (err: any) {
        console.error('上传语音消息失败:', err);
        alert(err.message || '语音发送失败');
      } finally {
        setIsUploading(false);
        cleanupAudio();
        onClose();
      }
    };

    mediaRecorderRef.current.stop();
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
      <motion.div
        initial={{ opacity: 0, scale: 0.9, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.9, y: 20 }}
        className="relative w-full max-w-sm p-6 rounded-3xl glass-card shadow-warm-lg flex flex-col items-center text-center"
      >
        {/* 顶部标题与呼吸灯 */}
        <div className="flex items-center gap-2 mb-2">
          <span className="w-2.5 h-2.5 rounded-full bg-rose-500 animate-ping" />
          <h3 className="font-bold text-base text-[#2E2419]">正在录制语音</h3>
        </div>

        <p className="text-xs text-[#A3927C] mb-6">
          最长支持 60 秒语音，松开或点击发送直接送达
        </p>

        {/* 麦浪实时跳动声波可视化柱状图 */}
        <div className="flex items-center justify-center gap-1.5 h-16 w-full px-4 mb-6 bg-[#F0EBE3] rounded-2xl border border-[#C9B99A]/40 shadow-inner">
          {liveAmplitudes.map((amp, idx) => (
            <motion.div
              key={idx}
              animate={{ height: `${Math.max(15, amp * 100)}%` }}
              transition={{ duration: 0.05 }}
              className="w-1.5 rounded-full bg-gradient-to-t from-[#8B7355] to-[#C9B99A]"
            />
          ))}
        </div>

        {/* 倒计时与秒数 */}
        <div className="text-2xl font-mono font-bold text-[#8B7355] mb-8">
          00:{recordDuration.toString().padStart(2, '0')}
        </div>

        {/* 底部控制按钮 */}
        <div className="flex items-center justify-center gap-6 w-full">
          {/* 取消按钮 */}
          <motion.button
            whileTap={{ scale: 0.9 }}
            onClick={handleCancel}
            disabled={isUploading}
            className="w-12 h-12 rounded-2xl bg-stone-200 hover:bg-rose-100 text-stone-600 hover:text-rose-600 flex items-center justify-center shadow-warm-sm transition-colors"
          >
            <Trash2 size={20} />
          </motion.button>

          {/* 发送按钮 */}
          <motion.button
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.92 }}
            onClick={handleStopAndSend}
            disabled={isUploading}
            className="w-14 h-14 rounded-2xl bg-gradient-to-tr from-[#8B7355] to-[#7A6348] text-white flex items-center justify-center shadow-walnut-glow disabled:opacity-50"
          >
            {isUploading ? (
              <Loader2 size={24} className="animate-spin" />
            ) : (
              <Send size={24} className="ml-0.5" />
            )}
          </motion.button>
        </div>
      </motion.div>
    </div>
  );
};
