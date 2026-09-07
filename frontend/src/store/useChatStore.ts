import { create } from 'zustand';
import { MsgType, WsMessagePayload, generateStanzaId } from '../proto/message';
import { wsClient } from '../socket/wsClient';
import { messageApi, groupApi } from '../api';
import { soundEffects } from '../audio/soundEffects';
import { useFriendStore } from './useFriendStore';

export interface ChatMessage {
  id: string;              // 本地或服务端消息唯一 ID
  stanza_id: string;       // 用于去重与 ACK 关联
  type: MsgType;
  from_uid: string;
  from_name: string;       // 清晰友好的用户名 (如 alice, bob)
  to_uid?: string;
  content: string;
  timestamp: number;
  seq: number;
  extra?: string;          // JSON 元数据
  status: 'sending' | 'success' | 'error';
  isSelf: boolean;
}

export interface Conversation {
  id: string;              // 'hall' | userId | groupId
  type: 'hall' | 'private' | 'group';
  name: string;
  avatar_url?: string;
  lastMessage?: string;
  unreadCount?: number;
}

export interface ChatState {
  activeConversation: Conversation;
  conversations: Conversation[];
  messages: Record<string, ChatMessage[]>; // covId -> ChatMessage[]
  
  setActiveConversation: (conv: Conversation) => void;
  sendTextMessage: (content: string, extra?: string) => Promise<void>;
  sendMediaMessage: (mediaUrl: string, mediaType: 'image' | 'voice' | 'video', extraData?: any) => Promise<void>;
  addIncomingMessage: (msg: WsMessagePayload, currentUserId: string) => void;
  loadHistory: (covId: string) => Promise<void>;
  updateMessageAck: (stanzaId: string, seq: number) => void;
}

const DEFAULT_CONVERSATION: Conversation = {
  id: 'hall',
  type: 'hall',
  name: '公共大厅',
  avatar_url: '',
};

export const useChatStore = create<ChatState>((set, get) => ({
  activeConversation: DEFAULT_CONVERSATION,
  conversations: [DEFAULT_CONVERSATION],
  messages: {
    hall: [],
  },

  setActiveConversation: (conv) => {
    set({ activeConversation: conv });
    const currentMessages = get().messages[conv.id];
    if (!currentMessages || currentMessages.length === 0) {
      get().loadHistory(conv.id);
    }
  },

  sendTextMessage: async (content: string, extra = '') => {
    const activeConv = get().activeConversation;
    const stanza_id = generateStanzaId();
    const currentUserId = localStorage.getItem('neo_user_id') || '';
    const currentUsername = localStorage.getItem('neo_username') || '我';

    // 1. 确定消息类型
    let msgType = MsgType.CHAT;
    let to_uid = '';
    if (activeConv.type === 'private') {
      msgType = MsgType.PRIVATE_CHAT;
      to_uid = activeConv.id;
    } else if (activeConv.type === 'group') {
      msgType = MsgType.GROUP_CHAT;
      to_uid = activeConv.id;
    }

    // 2. 将当前发送者的真实 Username 注入 extra 中，以便接收端直接展示
    let extraObj: any = {};
    if (extra) {
      try {
        extraObj = JSON.parse(extra);
      } catch {}
    }
    extraObj.from_username = currentUsername;
    const finalExtra = JSON.stringify(extraObj);

    // 3. 本地乐观消息
    const optimisticMsg: ChatMessage = {
      id: stanza_id,
      stanza_id,
      type: msgType,
      from_uid: currentUserId,
      from_name: currentUsername,
      to_uid,
      content,
      timestamp: Date.now(),
      seq: 0,
      extra: finalExtra,
      status: 'sending',
      isSelf: true,
    };

    const covId = activeConv.id;
    set((state) => ({
      messages: {
        ...state.messages,
        [covId]: [...(state.messages[covId] || []), optimisticMsg],
      },
    }));

    // 4. 通过 WebSocket 发送二进制数据
    try {
      soundEffects.playSendSound();
      const ack = await wsClient.sendMessage({
        type: msgType,
        to_uid,
        content,
        extra: finalExtra,
        stanza_id,
      });

      // 5. 发送成功，更新状态为 success 与 seq
      set((state) => {
        const list = state.messages[covId] || [];
        const updatedList = list.map((m) =>
          m.stanza_id === stanza_id
            ? { ...m, status: 'success' as const, seq: ack.seq }
            : m
        );
        return {
          messages: {
            ...state.messages,
            [covId]: updatedList,
          },
        };
      });
    } catch (err) {
      console.error('发送消息失败:', err);
      set((state) => {
        const list = state.messages[covId] || [];
        const updatedList = list.map((m) =>
          m.stanza_id === stanza_id ? { ...m, status: 'error' as const } : m
        );
        return {
          messages: {
            ...state.messages,
            [covId]: updatedList,
          },
        };
      });
    }
  },

  sendMediaMessage: async (mediaUrl: string, mediaType, extraData = {}) => {
    const extra = JSON.stringify({
      type: mediaType,
      url: mediaUrl,
      ...extraData,
    });
    const promptText = mediaType === 'image' ? '[图片]' : mediaType === 'voice' ? '[语音]' : '[视频]';
    return get().sendTextMessage(promptText, extra);
  },

  addIncomingMessage: (msg: WsMessagePayload, currentUserId: string) => {
    // 忽略心跳与回执
    if (
      msg.type === MsgType.HEARTBEAT_PING ||
      msg.type === MsgType.HEARTBEAT_PONG ||
      msg.type === MsgType.ACK
    ) {
      return;
    }

    // 计算所属会话 ID
    let covId = 'hall';
    if (msg.type === MsgType.PRIVATE_CHAT) {
      covId = msg.from_uid === currentUserId ? msg.to_uid || 'hall' : msg.from_uid || 'hall';
    } else if (msg.type === MsgType.GROUP_CHAT) {
      covId = msg.to_uid || 'hall';
    }

    const stanza_id = msg.stanza_id || generateStanzaId();
    const isSelf = msg.from_uid === currentUserId;

    // 避免乐观消息与推回消息重复
    const currentList = get().messages[covId] || [];
    const exists = currentList.some(
      (m) => m.stanza_id === stanza_id || (msg.seq && m.seq === Number(msg.seq) && m.seq > 0)
    );
    if (exists && isSelf) {
      return;
    }

    // 解析发送者清晰的用户名 (优先从 extra 读，其次从好友列表匹配)
    let fromUsername = '';
    if (msg.extra) {
      try {
        const parsed = JSON.parse(msg.extra);
        if (parsed.from_username) fromUsername = parsed.from_username;
      } catch {}
    }
    if (!fromUsername && msg.from_uid) {
      const friend = useFriendStore.getState().friends.find((f) => f.user_id === msg.from_uid);
      if (friend) {
        fromUsername = friend.username;
      }
    }

    const newMsg: ChatMessage = {
      id: stanza_id,
      stanza_id,
      type: msg.type,
      from_uid: msg.from_uid || '',
      from_name: fromUsername || msg.from_uid || '用户',
      to_uid: msg.to_uid || '',
      content: msg.content || '',
      timestamp: Number(msg.timestamp || Date.now()),
      seq: Number(msg.seq || 0),
      extra: msg.extra || '',
      status: 'success',
      isSelf,
    };

    set((state) => ({
      messages: {
        ...state.messages,
        [covId]: [...(state.messages[covId] || []), newMsg],
      },
    }));

    if (!isSelf) {
      soundEffects.playReceiveSound();
    }
  },

  loadHistory: async (covId: string) => {
    try {
      const activeConv = get().activeConversation;
      const currentUserId = localStorage.getItem('neo_user_id') || '';

      if (activeConv.type === 'group') {
        const res = await groupApi.getGroupHistory(covId, 0, 50);
        if (res.code === 0 && res.data && res.data.messages) {
          const list: ChatMessage[] = res.data.messages.map((m: any) => ({
            id: m.stanza_id || `hist_${m.seq}`,
            stanza_id: m.stanza_id || `hist_${m.seq}`,
            type: MsgType.GROUP_CHAT,
            from_uid: m.from_uid,
            from_name: m.from_uid,
            to_uid: m.to_uid,
            content: m.content,
            timestamp: m.timestamp,
            seq: m.seq,
            extra: m.extra,
            status: 'success',
            isSelf: m.from_uid === currentUserId,
          }));
          set((state) => ({
            messages: {
              ...state.messages,
              [covId]: list,
            },
          }));
        }
      } else if (activeConv.type === 'private') {
        const targetId = activeConv.id;
        const u1 = currentUserId < targetId ? currentUserId : targetId;
        const u2 = currentUserId < targetId ? targetId : currentUserId;
        const fullCovId = `cov:${u1}:${u2}`;

        const res = await messageApi.getHistory(fullCovId, 0, 50);
        if (res.code === 0 && res.data && res.data.messages) {
          const list: ChatMessage[] = res.data.messages.map((m) => ({
            id: m.stanza_id || `hist_${m.seq}`,
            stanza_id: m.stanza_id || `hist_${m.seq}`,
            type: MsgType.PRIVATE_CHAT,
            from_uid: m.from_uid,
            from_name: m.from_uid === currentUserId ? '我' : activeConv.name,
            to_uid: m.to_uid,
            content: m.content,
            timestamp: m.timestamp,
            seq: m.seq,
            extra: m.extra,
            status: 'success',
            isSelf: m.from_uid === currentUserId,
          }));
          set((state) => ({
            messages: {
              ...state.messages,
              [covId]: list,
            },
          }));
        }
      }
    } catch (err) {
      console.warn('拉取历史消息异常:', err);
    }
  },

  updateMessageAck: (stanzaId: string, seq: number) => {
    set((state) => {
      const nextMessages: Record<string, ChatMessage[]> = {};
      Object.keys(state.messages).forEach((covId) => {
        nextMessages[covId] = state.messages[covId].map((m) =>
          m.stanza_id === stanzaId ? { ...m, status: 'success', seq } : m
        );
      });
      return { messages: nextMessages };
    });
  },
}));
