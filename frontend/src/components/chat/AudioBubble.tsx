import React, { useRef, useState } from 'react';
import { motion } from 'framer-motion';
import { Play, Pause, Volume2 } from 'lucide-react';
import { useAudioStore } from '../../store';

interface AudioBubbleProps {
  audioUrl: string;
  duration: number;
  peaks?: number[];
  isSelf?: boolean;
}

export const AudioBubble: React.FC<AudioBubbleProps> = ({
  audioUrl,
  duration,
  peaks = [0.3, 0.5, 0.8, 0.4, 0.9, 0.6, 0.7, 0.3, 0.5, 0.8, 0.4, 0.6],
  isSelf = false,
}) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [progress, setProgress] = useState(0); // 0 ~ 1
  const audioRef = useRef<HTMLAudioElement | null>(null);

  const { activePlayingVoiceUrl, setActivePlayingVoiceUrl } = useAudioStore();

  const togglePlay = () => {
    if (!audioRef.current) return;

    if (isPlaying) {
      audioRef.current.pause();
      setIsPlaying(false);
      setActivePlayingVoiceUrl(null);
    } else {
      setActivePlayingVoiceUrl(audioUrl);
      audioRef.current.play();
      setIsPlaying(true);
    }
  };

  return (
    <div className="flex items-center gap-3 py-1 min-w-[200px]">
      {/* 播放/暂停控制按钮 */}
      <motion.button
        whileTap={{ scale: 0.88 }}
        whileHover={{ scale: 1.08 }}
        onClick={togglePlay}
        className={`w-8 h-8 rounded-full flex items-center justify-center transition-all ${
          isSelf
            ? 'bg-white/20 hover:bg-white/30 text-white'
            : 'bg-[#8B7355] text-white hover:bg-[#7A6348]'
        }`}
      >
        {isPlaying ? <Pause size={15} /> : <Play size={15} className="ml-0.5" />}
      </motion.button>

      {/* 麦浪跳动波形条 */}
      <div className="flex items-center gap-[3px] h-7 flex-1">
        {peaks.map((height, idx) => {
          const isPlayed = idx / peaks.length <= progress;
          return (
            <motion.div
              key={idx}
              initial={{ height: 6 }}
              animate={{
                height: isPlaying ? `${Math.max(20, Math.random() * 100)}%` : `${Math.max(20, height * 100)}%`,
                backgroundColor: isPlayed
                  ? isSelf ? '#FAF8F5' : '#8B7355'
                  : isSelf ? 'rgba(250, 248, 245, 0.35)' : 'rgba(201, 185, 154, 0.5)',
              }}
              transition={{ duration: 0.15 }}
              className="w-[3px] rounded-full"
            />
          );
        })}
      </div>

      {/* 语音秒数 */}
      <span className={`text-xs font-mono font-medium ${isSelf ? 'text-white/90' : 'text-[#8B7355]'}`}>
        {duration}"
      </span>

      <audio
        ref={audioRef}
        src={audioUrl}
        onTimeUpdate={(e) => {
          const cur = e.currentTarget.currentTime;
          const dur = e.currentTarget.duration || duration;
          setProgress(dur > 0 ? cur / dur : 0);
        }}
        onEnded={() => {
          setIsPlaying(false);
          setProgress(0);
          setActivePlayingVoiceUrl(null);
        }}
      />
    </div>
  );
};
