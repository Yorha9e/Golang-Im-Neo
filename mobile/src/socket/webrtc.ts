import { wsClient } from './wsClient';
import { MsgType, WsMessagePayload } from '../proto/message';
import { useAudioStore } from '../store';
import { soundEffects } from '../audio/soundEffects';

class WebRtcEngine {
  private peerConnection: RTCPeerConnection | null = null;
  private localStream: MediaStream | null = null;
  private remoteAudio: HTMLAudioElement | null = null;
  private targetUserId = '';
  private isCaller = false;

  // 缓冲队列：防止在 setRemoteDescription 之前收到的 Candidate 被丢弃
  private pendingCandidates: RTCIceCandidateInit[] = [];

  // 国内高可用极速 STUN 服务器池 (腾讯云 / 小米 / Bilibili)
  private configuration: RTCConfiguration = {
    iceServers: [
      { urls: 'stun:stun.qq.com:3478' },
      { urls: 'stun:stun.miwifi.com:3478' },
      { urls: 'stun:stun.chat.bilibili.com:3478' },
    ],
    iceCandidatePoolSize: 10,
  };

  constructor() {
    // 监听 WebSocket 下推信令
    wsClient.onMessage((msg: WsMessagePayload) => {
      this.handleSignalingMessage(msg);
    });
  }

  /**
   * 发起通话 (Caller)
   */
  public async call(targetUserId: string, targetUsername: string) {
    this.targetUserId = targetUserId;
    this.isCaller = true;
    this.pendingCandidates = [];

    console.log('[WebRTC] 正在发起通话 ->', targetUsername, targetUserId);
    useAudioStore.getState().startCall(targetUserId, targetUsername);

    try {
      this.localStream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });

      this.initPeerConnection();

      // 添加本地音频轨
      this.localStream.getTracks().forEach((track) => {
        this.peerConnection?.addTrack(track, this.localStream!);
      });

      // 创建 Offer
      const offer = await this.peerConnection!.createOffer();
      await this.peerConnection!.setLocalDescription(offer);

      console.log('[WebRTC] Offer SDP 创建成功，通过 WS 发送信令...');

      // 通过 WS 发送 Offer 信令
      await wsClient.sendMessage({
        type: MsgType.SIGNALING_OFFER,
        to_uid: targetUserId,
        content: JSON.stringify(offer),
      });
    } catch (err) {
      console.error('[WebRTC] 发起通话失败:', err);
      alert('无法获取麦克风权限，无法发起通话');
      this.hangup();
    }
  }

  /**
   * 用户点击【接听】来电 (在点击手势中唤起麦克风权限)
   */
  public async acceptIncomingCall() {
    const { incomingCall } = useAudioStore.getState();
    if (!incomingCall || !incomingCall.offerSdp) return;

    const fromUid = incomingCall.fromUserId;
    const fromName = incomingCall.fromUsername;
    const offerSdp = incomingCall.offerSdp;

    useAudioStore.getState().clearIncomingCall();
    useAudioStore.getState().startCall(fromUid, fromName);

    await this.answer(fromUid, offerSdp);
  }

  /**
   * 用户点击【拒绝】来电
   */
  public rejectIncomingCall() {
    useAudioStore.getState().clearIncomingCall();
    this.hangup();
  }

  /**
   * 内部处理接听流程
   */
  private async answer(targetUserId: string, offerSdp: RTCSessionDescriptionInit) {
    this.targetUserId = targetUserId;
    this.isCaller = false;
    this.pendingCandidates = [];

    console.log('[WebRTC] 用户已确认接听，正在协商 ->', targetUserId);

    try {
      this.localStream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });

      this.initPeerConnection();

      this.localStream.getTracks().forEach((track) => {
        this.peerConnection?.addTrack(track, this.localStream!);
      });

      // 1. 设置远端 Offer
      await this.peerConnection!.setRemoteDescription(new RTCSessionDescription(offerSdp));
      console.log('[WebRTC] RemoteDescription (Offer) 设置成功，刷新缓冲队列...');

      // 2. 刷新由于时序过早到达的 Candidate
      await this.flushPendingCandidates();

      // 3. 创建并设置本地 Answer
      const answer = await this.peerConnection!.createAnswer();
      await this.peerConnection!.setLocalDescription(answer);

      // 4. 发送 Answer 信令
      await wsClient.sendMessage({
        type: MsgType.SIGNALING_ANSWER,
        to_uid: targetUserId,
        content: JSON.stringify(answer),
      });

      useAudioStore.getState().setCallConnected();
    } catch (err) {
      console.error('[WebRTC] 接听通话失败:', err);
      alert('接听通话失败：麦克风权限受限或对端已断开');
      this.hangup();
    }
  }

  /**
   * 静音控制
   */
  public toggleMute(muted: boolean) {
    if (this.localStream) {
      this.localStream.getAudioTracks().forEach((t) => (t.enabled = !muted));
      useAudioStore.getState().setCallMuted(muted);
    }
  }

  /**
   * 挂断通话
   */
  public hangup() {
    console.log('[WebRTC] 挂断通话');
    if (this.peerConnection) {
      this.peerConnection.close();
      this.peerConnection = null;
    }
    if (this.localStream) {
      this.localStream.getTracks().forEach((t) => t.stop());
      this.localStream = null;
    }
    if (this.remoteAudio) {
      this.remoteAudio.pause();
      this.remoteAudio.srcObject = null;
      this.remoteAudio = null;
    }
    this.pendingCandidates = [];
    this.targetUserId = '';
    useAudioStore.getState().endCall();
  }

  private initPeerConnection() {
    this.peerConnection = new RTCPeerConnection(this.configuration);

    // 1. 发现本地网络候选地址 (局域网 IP / STUN 公网 IP)
    this.peerConnection.onicecandidate = (event) => {
      if (event.candidate && this.targetUserId) {
        console.log('[WebRTC] 发现本地候选地址 ->', event.candidate.candidate);
        wsClient.sendMessage({
          type: MsgType.SIGNALING_CANDIDATE,
          to_uid: this.targetUserId,
          content: JSON.stringify(event.candidate),
        });
      }
    };

    // 2. 连接状态监听
    this.peerConnection.onconnectionstatechange = () => {
      console.log('[WebRTC] 连接状态变更 ->', this.peerConnection?.connectionState);
      if (this.peerConnection?.connectionState === 'connected') {
        useAudioStore.getState().setCallConnected();
      } else if (
        this.peerConnection?.connectionState === 'disconnected' ||
        this.peerConnection?.connectionState === 'failed' ||
        this.peerConnection?.connectionState === 'closed'
      ) {
        this.hangup();
      }
    };

    // 3. 接收远端音频流并播放
    this.peerConnection.ontrack = (event) => {
      console.log('[WebRTC] 收到远端音频流，准备播放...', event.streams);
      if (!this.remoteAudio) {
        this.remoteAudio = new Audio();
        this.remoteAudio.autoplay = true;
      }
      this.remoteAudio.srcObject = event.streams[0];
      this.remoteAudio.play().catch((e) => {
        console.warn('[WebRTC] 自动播放受阻，等待用户交互触发:', e);
      });
      useAudioStore.getState().setCallConnected();
    };
  }

  /**
   * 处理 WebSocket 下推信令
   */
  private async handleSignalingMessage(msg: WsMessagePayload) {
    try {
      if (msg.type === MsgType.SIGNALING_OFFER && msg.content) {
        const offer = JSON.parse(msg.content);
        console.log('[WebRTC] 收到来自对方的语音呼叫 Offer ->', msg.from_uid);
        soundEffects.playNotifySound();
        // 弹出待接听来电弹窗
        useAudioStore.getState().setIncomingCall({
          fromUserId: msg.from_uid || '',
          fromUsername: msg.from_uid || '好友',
          offerSdp: offer,
        });
      } else if (msg.type === MsgType.SIGNALING_ANSWER && msg.content) {
        const answer = JSON.parse(msg.content);
        if (this.peerConnection) {
          console.log('[WebRTC] 收到 Answer，双方建立直连...');
          await this.peerConnection.setRemoteDescription(new RTCSessionDescription(answer));
          await this.flushPendingCandidates();
          useAudioStore.getState().setCallConnected();
        }
      } else if (msg.type === MsgType.SIGNALING_CANDIDATE && msg.content) {
        const candidate = JSON.parse(msg.content);
        if (this.peerConnection && this.peerConnection.remoteDescription) {
          await this.peerConnection.addIceCandidate(new RTCIceCandidate(candidate));
        } else {
          // 远端描述尚未就绪，暂存进队列
          console.log('[WebRTC] 暂存过早到达的 Candidate...');
          this.pendingCandidates.push(candidate);
        }
      }
    } catch (err) {
      console.error('[WebRTC] 处理信令异常:', err);
    }
  }

  /**
   * 刷新暂存的候选地址队列
   */
  private async flushPendingCandidates() {
    if (!this.peerConnection) return;
    while (this.pendingCandidates.length > 0) {
      const cand = this.pendingCandidates.shift();
      if (cand) {
        try {
          await this.peerConnection.addIceCandidate(new RTCIceCandidate(cand));
          console.log('[WebRTC] 成功注入暂存 Candidate');
        } catch (e) {
          console.warn('[WebRTC] 注入 Candidate 异常:', e);
        }
      }
    }
  }
}

export const webRtcEngine = new WebRtcEngine();
