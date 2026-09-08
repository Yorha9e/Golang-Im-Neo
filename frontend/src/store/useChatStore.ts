import { create } from 'zustand';
import { MsgType, WsMessagePayload, generateStanzaId } from '../proto/message';
import { wsClient } from '../socket/wsClient';
import { messageApi, groupApi } from '../api';
import { soundEffects } from '../audio/soundEffects';
import { useFriendStore } from './useFriendStore';
import { useAuthStore } from './useAuthStore';

export interface ChatMessage {
  id: string;              // 本地或服务端消息唯一 ID
  stanza_id: string;       // 用于去重与 ACK 关联
  type: MsgType;
  from_uid: string;
  from_name: string;       // 清晰友好的用户名 (如 alice, bob)
  from_role?: string;      // 发送者身份角色 (superadmin, admin, user)
  from_avatar?: string;    // 发送者头像 URL
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

export interface SystemNoticeItem {
  id: string;
  content: string;
  timestamp: number;
}

export interface ChatState {
  activeConversation: Conversation;
  conversations: Conversation[];
  messages: Record<string, ChatMessage[]>; // covId -> ChatMessage[]
  
  // 系统公告通知中心
  systemNotices: SystemNoticeItem[];
  activeBroadcastNotice: SystemNoticeItem | null;
  hasUnreadNotice: boolean;
  dismissBroadcastNotice: () => void;
  clearSystemNotices: () => void;
  markNoticesAsRead: () => void;

  setActiveConversation: (conv: Conversation) => void;
  sendTextMessage: (content: string, extra?: string) => Promise<void>;
  sendMediaMessage: (mediaUrl: string, mediaType: 'image' | 'voice' | 'video', extraData?: any) => Promise<void>;
  addIncomingMessage: (msg: WsMessagePayload, currentUserId: string) => void;
  loadHistory: (covId: string, isLoadMore?: boolean) => Promise<void>;
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

  systemNotices: [],
  activeBroadcastNotice: null,
  hasUnreadNotice: false,

  dismissBroadcastNotice: () => set({ activeBroadcastNotice: null }),
  
  clearSystemNotices: () => {
    set({ systemNotices: [], hasUnreadNotice: false });
    localStorage.setItem('neo_last_read_notice_time', Date.now().toString());
  },

  markNoticesAsRead: () => {
    set({ hasUnreadNotice: false });
    localStorage.setItem('neo_last_read_notice_time', Date.now().toString());
  },

  setActiveConversation: (conv) => {
    set({ activeConversation: conv });
    const currentMessages = get().messages[conv.id];
    if (!currentMessages || currentMessages.length === 0) {
      get().loadHistory(conv.id, false);
    }
  },

  sendTextMessage: async (content: string, extra = '') => {
    const activeConv = get().activeConversation;
    const stanza_id = generateStanzaId();
    const currentUserId = localStorage.getItem('neo_user_id') || '';
    const currentUsername = localStorage.getItem('neo_username') || '我';
    const currentRole = useAuthStore.getState().role || 'user';
    const currentAvatar = useAuthStore.getState().avatarUrl || '';

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

    // 2. 将当前发送者的真实 Username, Role 与 Avatar 注入 extra 中
    let extraObj: any = {};
    if (extra) {
      try {
        extraObj = JSON.parse(extra);
      } catch {}
    }
    extraObj.from_username = currentUsername;
    extraObj.from_role = currentRole;
    if (currentAvatar) extraObj.from_avatar = currentAvatar;
    const finalExtra = JSON.stringify(extraObj);

    // 3. 本地乐观消息
    const optimisticMsg: ChatMessage = {
      id: stanza_id,
      stanza_id,
      type: msgType,
      from_uid: currentUserId,
      from_name: currentUsername,
      from_role: currentRole,
      from_avatar: currentAvatar,
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

    const stanza_id = msg.stanza_id || generateStanzaId();
    const timestamp = Number(msg.timestamp || Date.now());

    // 1. 处理全服系统广播公告 (SYSTEM_NOTICE)
    if (msg.type === MsgType.SYSTEM_NOTICE) {
      const noticeItem: SystemNoticeItem = {
        id: stanza_id,
        content: msg.content || '系统通知',
        timestamp,
      };

      // 触发顶部悬浮流光横幅、未读红点与通知音
      soundEffects.playNotifySound();
      set((state) => ({
        activeBroadcastNotice: noticeItem,
        hasUnreadNotice: true,
        systemNotices: [noticeItem, ...state.systemNotices.filter((n) => n.id !== stanza_id)],
      }));

      // 同时注入大厅消息流作为居中系统卡片
      const sysMsg: ChatMessage = {
        id: stanza_id,
        stanza_id,
        type: MsgType.SYSTEM_NOTICE,
        from_uid: 'system',
        from_name: '系统官方公告',
        from_role: 'superadmin',
        content: msg.content || '',
        timestamp,
        seq: Number(msg.seq || 0),
        status: 'success',
        isSelf: false,
      };

      set((state) => ({
        messages: {
          ...state.messages,
          hall: [...(state.messages['hall'] || []), sysMsg],
        },
      }));
      return;
    }

    // 2. 计算常规聊天所属会话 ID
    let covId = 'hall';
    if (msg.type === MsgType.PRIVATE_CHAT) {
      covId = msg.from_uid === currentUserId ? msg.to_uid || 'hall' : msg.from_uid || 'hall';
    } else if (msg.type === MsgType.GROUP_CHAT) {
      covId = msg.to_uid || 'hall';
    }

    const isSelf = msg.from_uid === currentUserId;

    // 避免乐观消息与推回消息重复
    const currentList = get().messages[covId] || [];
    const exists = currentList.some(
      (m) => m.stanza_id === stanza_id || (msg.seq && m.seq === Number(msg.seq) && m.seq > 0)
    );
    if (exists && isSelf) {
      return;
    }

    // 解析发送者用户名、角色与头像
    let fromUsername = '';
    let fromRole = 'user';
    let fromAvatar = '';
    if (msg.extra) {
      try {
        const parsed = JSON.parse(msg.extra);
        if (parsed.from_username) fromUsername = parsed.from_username;
        if (parsed.from_role) fromRole = parsed.from_role;
        if (parsed.from_avatar) fromAvatar = parsed.from_avatar;
      } catch {}
    }
    if (msg.from_uid) {
      const friend = useFriendStore.getState().friends.find((f) => f.user_id === msg.from_uid);
      if (friend) {
        if (!fromUsername) fromUsername = friend.username;
        if (!fromAvatar && friend.avatar_url) fromAvatar = friend.avatar_url;
      }
    }

    const newMsg: ChatMessage = {
      id: stanza_id,
      stanza_id,
      type: msg.type,
      from_uid: msg.from_uid || '',
      from_name: fromUsername || msg.from_uid || '用户',
      from_role: fromRole,
      from_avatar: fromAvatar,
      to_uid: msg.to_uid || '',
      content: msg.content || '',
      timestamp,
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

  loadHistory: async (covId: string, isLoadMore = false) => {
    try {
      const activeConv = get().activeConversation;
      const currentUserId = localStorage.getItem('neo_user_id') || '';
      const existingList = get().messages[covId] || [];
      
      let beforeSeq = 0;
      if (isLoadMore && existingList.length > 0) {
        const validSeqs = existingList.map((m) => m.seq).filter((s) => s > 0);
        if (validSeqs.length > 0) {
          beforeSeq = Math.min(...validSeqs);
        }
      }

      let rawMessages: any[] = [];

      if (activeConv.type === 'hall') {
        const res = await messageApi.getHallHistory(beforeSeq, 50);
        if (res.code === 0 && res.data && res.data.messages) {
          rawMessages = res.data.messages;
        }
      } else if (activeConv.type === 'private') {
        const res = await messageApi.getPrivateHistory(activeConv.id, beforeSeq, 50);
        if (res.code === 0 && res.data && res.data.messages) {
          rawMessages = res.data.messages;
        }
      } else if (activeConv.type === 'group') {
        const res = await groupApi.getGroupHistory(covId, beforeSeq, 50);
        if (res.code === 0 && res.data && res.data.messages) {
          rawMessages = res.data.messages;
        }
      }

      if (rawMessages.length === 0) return;

      const friends = useFriendStore.getState().friends;
      const formattedHistory: ChatMessage[] = rawMessages.map((m: any) => {
        let fromName = '';
        let fromRole = 'user';
        let fromAvatar = '';
        if (m.extra) {
          try {
            const parsed = JSON.parse(m.extra);
            if (parsed.from_username) fromName = parsed.from_username;
            if (parsed.from_role) fromRole = parsed.from_role;
            if (parsed.from_avatar) fromAvatar = parsed.from_avatar;
          } catch {}
        }
        if (m.from_uid) {
          if (m.from_uid === currentUserId) {
            fromName = '我';
            fromRole = useAuthStore.getState().role || 'user';
            fromAvatar = useAuthStore.getState().avatarUrl || '';
          } else {
            const f = friends.find((fr) => fr.user_id === m.from_uid);
            if (f) {
              if (!fromName) fromName = f.username;
              if (!fromAvatar && f.avatar_url) fromAvatar = f.avatar_url;
            } else if (activeConv.type === 'private' && m.from_uid === activeConv.id) {
              if (!fromName) fromName = activeConv.name;
            }
          }
        }

        const isSelf = m.from_uid === currentUserId;

        return {
          id: m.stanza_id || `hist_${m.seq}`,
          stanza_id: m.stanza_id || `hist_${m.seq}`,
          type: activeConv.type === 'hall' ? MsgType.CHAT : activeConv.type === 'private' ? MsgType.PRIVATE_CHAT : MsgType.GROUP_CHAT,
          from_uid: m.from_uid || '',
          from_name: fromName || m.from_uid || '用户',
          from_role: fromRole,
          from_avatar: fromAvatar,
          to_uid: m.to_uid || '',
          content: m.content || '',
          timestamp: Number(m.timestamp || Date.now()),
          seq: Number(m.seq || 0),
          extra: m.extra || '',
          status: 'success',
          isSelf,
        };
      });

      set((state) => {
        const currentList = isLoadMore ? (state.messages[covId] || []) : [];
        const map = new Map<string, ChatMessage>();
        
        [...formattedHistory, ...currentList].forEach((item) => {
          const key = item.stanza_id || `seq_${item.seq}`;
          map.set(key, item);
        });

        const merged = Array.from(map.values()).sort((a, b) => a.seq - b.seq || a.timestamp - b.timestamp);

        return {
          messages: {
            ...state.messages,
            [covId]: merged,
          },
        };
      });
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
