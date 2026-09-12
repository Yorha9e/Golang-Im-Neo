import { create } from 'zustand';

export interface AudioState {
  soundEnabled: boolean;
  soundVolume: number;           // 0 ~ 1
  activePlayingVoiceUrl: string | null;
  isVoiceRecording: boolean;
  
  // 待接听的来电信息
  incomingCall: {
    isOpen: boolean;
    fromUserId: string;
    fromUsername: string;
    offerSdp: any | null;
  } | null;

  // 正在进行的实时语音通话状态
  webRtcCall: {
    isOpen: boolean;
    targetUserId: string;
    targetUsername: string;
    status: 'idle' | 'calling' | 'connected';
    isMuted: boolean;
    isSpeakerTalking: boolean;
  };

  setSoundEnabled: (enabled: boolean) => void;
  setSoundVolume: (vol: number) => void;
  setActivePlayingVoiceUrl: (url: string | null) => void;
  setIsVoiceRecording: (rec: boolean) => void;

  setIncomingCall: (call: { fromUserId: string; fromUsername: string; offerSdp: any } | null) => void;
  clearIncomingCall: () => void;

  startCall: (targetUserId: string, targetUsername: string) => void;
  setCallConnected: () => void;
  setCallMuted: (muted: boolean) => void;
  setSpeakerTalking: (talking: boolean) => void;
  endCall: () => void;
}

export const useAudioStore = create<AudioState>((set) => ({
  soundEnabled: true,
  soundVolume: 0.7,
  activePlayingVoiceUrl: null,
  isVoiceRecording: false,

  incomingCall: null,

  webRtcCall: {
    isOpen: false,
    targetUserId: '',
    targetUsername: '',
    status: 'idle',
    isMuted: false,
    isSpeakerTalking: false,
  },

  setSoundEnabled: (enabled) => set({ soundEnabled: enabled }),
  setSoundVolume: (vol) => set({ soundVolume: vol }),
  setActivePlayingVoiceUrl: (url) => set({ activePlayingVoiceUrl: url }),
  setIsVoiceRecording: (rec) => set({ isVoiceRecording: rec }),

  setIncomingCall: (call) =>
    set({
      incomingCall: call ? { isOpen: true, ...call } : null,
    }),

  clearIncomingCall: () => set({ incomingCall: null }),

  startCall: (targetUserId, targetUsername) =>
    set({
      webRtcCall: {
        isOpen: true,
        targetUserId,
        targetUsername,
        status: 'calling',
        isMuted: false,
        isSpeakerTalking: false,
      },
    }),

  setCallConnected: () =>
    set((state) => ({
      webRtcCall: {
        ...state.webRtcCall,
        status: 'connected',
      },
    })),

  setCallMuted: (muted) =>
    set((state) => ({
      webRtcCall: {
        ...state.webRtcCall,
        isMuted: muted,
      },
    })),

  setSpeakerTalking: (talking) =>
    set((state) => ({
      webRtcCall: {
        ...state.webRtcCall,
        isSpeakerTalking: talking,
      },
    })),

  endCall: () =>
    set({
      incomingCall: null,
      webRtcCall: {
        isOpen: false,
        targetUserId: '',
        targetUsername: '',
        status: 'idle',
        isMuted: false,
        isSpeakerTalking: false,
      },
    }),
}));
