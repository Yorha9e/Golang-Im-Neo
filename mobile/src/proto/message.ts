import protobuf from 'protobufjs';

/**
 * WebSocket 消息类型枚举 (完全对齐后端的 proto/message.proto)
 */
export enum MsgType {
  UNKNOWN = 0,
  HEARTBEAT_PING = 1,          // 客户端心跳保活
  HEARTBEAT_PONG = 2,          // 服务端心跳响应
  CHAT = 3,                    // 公聊大厅广播
  PRIVATE_CHAT = 4,            // 私聊单聊
  SYSTEM_NOTICE = 5,           // 全局系统通知
  ACK = 6,                     // 消息送达回执 (stanza_id + seq)
  FRIEND_APPLY_NOTIFY = 10,    // 收到好友申请实时通知
  FRIEND_ACCEPT_NOTIFY = 11,   // 好友申请通过实时通知
  SIGNALING_OFFER = 20,        // WebRTC Offer 信令
  SIGNALING_ANSWER = 21,       // WebRTC Answer 信令
  SIGNALING_CANDIDATE = 22,    // WebRTC ICE Candidate 信令
  GROUP_CHAT = 30,             // 群聊消息
}

/**
 * 核心信封数据接口
 */
export interface WsMessagePayload {
  type: MsgType;
  seq?: number | string;
  from_uid?: string;
  to_uid?: string;
  content?: string;
  timestamp?: number | string;
  payload?: Uint8Array | null;
  extra?: string;
  stanza_id?: string;
}

// 动态构建 Protobuf Root 树 (确保零外部依赖、浏览器端高性能解析)
const root = new protobuf.Root();

const MsgTypeEnum = new protobuf.Enum('MsgType', {
  UNKNOWN: 0,
  HEARTBEAT_PING: 1,
  HEARTBEAT_PONG: 2,
  CHAT: 3,
  PRIVATE_CHAT: 4,
  SYSTEM_NOTICE: 5,
  ACK: 6,
  FRIEND_APPLY_NOTIFY: 10,
  FRIEND_ACCEPT_NOTIFY: 11,
  SIGNALING_OFFER: 20,
  SIGNALING_ANSWER: 21,
  SIGNALING_CANDIDATE: 22,
  GROUP_CHAT: 30,
});

const WsMessageType = new protobuf.Type('WsMessage')
  .add(new protobuf.Field('type', 1, 'int32'))
  .add(new protobuf.Field('seq', 2, 'int64'))
  .add(new protobuf.Field('from_uid', 3, 'string'))
  .add(new protobuf.Field('to_uid', 4, 'string'))
  .add(new protobuf.Field('content', 5, 'string'))
  .add(new protobuf.Field('timestamp', 6, 'int64'))
  .add(new protobuf.Field('payload', 7, 'bytes'))
  .add(new protobuf.Field('extra', 8, 'string'))
  .add(new protobuf.Field('stanza_id', 9, 'string'));

root.add(MsgTypeEnum);
root.add(WsMessageType);

/**
 * 编码 WsMessage 为 Uint8Array 二进制 Buffer
 */
export function encodeWsMessage(message: WsMessagePayload): Uint8Array {
  const errMsg = WsMessageType.verify(message);
  if (errMsg) {
    throw Error(`Protobuf verification failed: ${errMsg}`);
  }
  const msgObj = WsMessageType.create(message);
  return WsMessageType.encode(msgObj).finish();
}

/**
 * 解码 Uint8Array / ArrayBuffer 为 WsMessage 对象
 */
export function decodeWsMessage(buffer: Uint8Array | ArrayBuffer): WsMessagePayload {
  const uint8 = buffer instanceof Uint8Array ? buffer : new Uint8Array(buffer);
  const decoded = WsMessageType.decode(uint8) as any;
  return {
    type: decoded.type as MsgType,
    seq: typeof decoded.seq === 'object' && decoded.seq?.toNumber ? decoded.seq.toNumber() : Number(decoded.seq || 0),
    from_uid: decoded.from_uid || '',
    to_uid: decoded.to_uid || '',
    content: decoded.content || '',
    timestamp: typeof decoded.timestamp === 'object' && decoded.timestamp?.toNumber ? decoded.timestamp.toNumber() : Number(decoded.timestamp || Date.now()),
    payload: decoded.payload && decoded.payload.length > 0 ? decoded.payload : null,
    extra: decoded.extra || '',
    stanza_id: decoded.stanza_id || '',
  };
}

/**
 * 生成唯一 stanza_id
 */
export function generateStanzaId(): string {
  return 'stz_' + Date.now().toString(36) + '_' + Math.random().toString(36).substring(2, 9);
}
