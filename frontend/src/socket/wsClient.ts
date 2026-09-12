import { authApi } from '../api/auth';
import {
  MsgType,
  WsMessagePayload,
  encodeWsMessage,
  decodeWsMessage,
  generateStanzaId,
} from '../proto/message';

export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

type MessageListener = (msg: WsMessagePayload) => void;
type StatusListener = (status: ConnectionStatus) => void;
type AckListener = (stanzaId: string, seq: number) => void;

class WebSocketClient {
  private ws: WebSocket | null = null;
  private status: ConnectionStatus = 'disconnected';
  private reconnectAttempt = 0;
  private maxReconnectDelay = 15000;
  private reconnectTimer: any = null;
  private heartbeatTimer: any = null;
  private heartbeatTimeoutTimer: any = null;
  private heartbeatInterval = 25000; // 25s 发一次 ping
  private heartbeatTimeout = 10000;  // 10s 未回 pong 则判定断线

  // 待确认的 ACK 字典：stanza_id -> { resolve, reject, timeoutTimer }
  private pendingAcks = new Map<string, {
    resolve: (seq: number) => void;
    reject: (err: Error) => void;
    timer: any;
  }>();

  // 监听器集合
  private messageListeners = new Set<MessageListener>();
  private statusListeners = new Set<StatusListener>();
  private ackListeners = new Set<AckListener>();

  constructor() {
    // 监听认证失效事件
    if (typeof window !== 'undefined') {
      window.addEventListener('auth:expired', () => {
        this.disconnect();
      });
    }
  }

  public getStatus(): ConnectionStatus {
    return this.status;
  }

  private setStatus(newStatus: ConnectionStatus) {
    if (this.status !== newStatus) {
      this.status = newStatus;
      this.statusListeners.forEach((listener) => listener(newStatus));
    }
  }

  /**
   * 发起连接
   */
  public async connect(): Promise<void> {
    if (this.status === 'connected' || this.status === 'connecting') {
      return;
    }

    this.setStatus('connecting');

    try {
      // 1. 获取一次性票据 Ticket
      const ticketRes = await authApi.getTicket();
      if (ticketRes.code !== 0 || !ticketRes.data.ticket) {
        throw new Error(ticketRes.msg || '获取 WebSocket 票据失败');
      }

      const ticket = ticketRes.data.ticket;
      
      // 2. 拼接 WS 协议地址 (兼容开发代理、生产同源与客户端直连)
      let wsUrl = '';
      if (typeof window !== 'undefined' && (window.location.protocol === 'file:' || (window as any).electronAPI?.isElectron)) {
        wsUrl = `wss://106.52.170.56:8080/ws?ticket=${encodeURIComponent(ticket)}`;
      } else {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        wsUrl = `${protocol}//${host}/ws?ticket=${encodeURIComponent(ticket)}`;
      }

      this.ws = new WebSocket(wsUrl);
      this.ws.binaryType = 'arraybuffer';

      this.ws.onopen = () => {
        this.setStatus('connected');
        this.reconnectAttempt = 0;
        this.startHeartbeat();
      };

      this.ws.onmessage = (event: MessageEvent) => {
        try {
          if (event.data instanceof ArrayBuffer) {
            const msg = decodeWsMessage(event.data);
            this.handleIncomingMessage(msg);
          }
        } catch (err) {
          console.error('[WS] 解码二进制消息异常:', err);
        }
      };

      this.ws.onerror = (error) => {
        console.error('[WS] 连接异常:', error);
      };

      this.ws.onclose = () => {
        this.stopHeartbeat();
        this.setStatus('disconnected');
        this.scheduleReconnect();
      };
    } catch (err) {
      console.error('[WS] 握手前置失败:', err);
      this.setStatus('disconnected');
      this.scheduleReconnect();
    }
  }

  /**
   * 主动断开连接
   */
  public disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.stopHeartbeat();
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
    this.setStatus('disconnected');
  }

  /**
   * 发送业务消息 (支持 ACK 异步等待)
   */
  public async sendMessage(
    msg: Omit<WsMessagePayload, 'stanza_id' | 'timestamp' | 'seq'> & { stanza_id?: string },
    waitForAck = true,
    timeoutMs = 6000
  ): Promise<{ stanza_id: string; seq: number }> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket 未连接，无法发送消息');
    }

    const stanza_id = msg.stanza_id || generateStanzaId();
    const payload: WsMessagePayload = {
      ...msg,
      stanza_id,
      seq: 0,
      timestamp: Date.now(),
    };

    const encoded = encodeWsMessage(payload);
    this.ws.send(encoded);

    if (!waitForAck || msg.type === MsgType.HEARTBEAT_PING) {
      return { stanza_id, seq: 0 };
    }

    return new Promise<{ stanza_id: string; seq: number }>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingAcks.delete(stanza_id);
        reject(new Error('发送超时，未收到服务器 ACK 回执'));
      }, timeoutMs);

      this.pendingAcks.set(stanza_id, {
        resolve: (seq: number) => {
          clearTimeout(timer);
          this.pendingAcks.delete(stanza_id);
          resolve({ stanza_id, seq });
        },
        reject: (err) => {
          clearTimeout(timer);
          this.pendingAcks.delete(stanza_id);
          reject(err);
        },
        timer,
      });
    });
  }

  /**
   * 处理接收到的所有消息
   */
  private handleIncomingMessage(msg: WsMessagePayload) {
    // 1. 处理心跳 PONG 回执
    if (msg.type === MsgType.HEARTBEAT_PONG) {
      if (this.heartbeatTimeoutTimer) {
        clearTimeout(this.heartbeatTimeoutTimer);
        this.heartbeatTimeoutTimer = null;
      }
      return;
    }

    // 2. 处理消息 ACK
    if (msg.type === MsgType.ACK && msg.stanza_id) {
      const seq = Number(msg.seq || 0);
      const pending = this.pendingAcks.get(msg.stanza_id);
      if (pending) {
        pending.resolve(seq);
      }
      this.ackListeners.forEach((listener) => listener(msg.stanza_id!, seq));
      return;
    }

    // 3. 广播给所有业务监听器
    this.messageListeners.forEach((listener) => listener(msg));
  }

  /**
   * 心跳定时器控制
   */
  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        try {
          const ping = encodeWsMessage({
            type: MsgType.HEARTBEAT_PING,
            timestamp: Date.now(),
            stanza_id: generateStanzaId(),
          });
          this.ws.send(ping);

          // 开启 PONG 超时检测
          this.heartbeatTimeoutTimer = setTimeout(() => {
            console.warn('[WS] 心跳超时未收到 PONG，强制重连');
            this.ws?.close();
          }, this.heartbeatTimeout);
        } catch (err) {
          console.error('[WS] 发送心跳失败:', err);
        }
      }
    }, this.heartbeatInterval);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    if (this.heartbeatTimeoutTimer) clearTimeout(this.heartbeatTimeoutTimer);
    this.heartbeatTimer = null;
    this.heartbeatTimeoutTimer = null;
  }

  /**
   * 指数退避重连调度
   */
  private scheduleReconnect() {
    if (this.reconnectTimer) return;

    this.setStatus('reconnecting');
    const delay = Math.min(1000 * Math.pow(1.8, this.reconnectAttempt), this.maxReconnectDelay);
    this.reconnectAttempt++;

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  /**
   * 监听事件注册
   */
  public onMessage(listener: MessageListener): () => void {
    this.messageListeners.add(listener);
    return () => this.messageListeners.delete(listener);
  }

  public onStatus(listener: StatusListener): () => void {
    this.statusListeners.add(listener);
    listener(this.status);
    return () => this.statusListeners.delete(listener);
  }

  public onAck(listener: AckListener): () => void {
    this.ackListeners.add(listener);
    return () => this.ackListeners.delete(listener);
  }
}

export const wsClient = new WebSocketClient();
