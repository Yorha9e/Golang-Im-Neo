import React, { useEffect, useState } from 'react';
import { useAuthStore, useChatStore, useFriendStore } from './store';
import { wsClient } from './socket/wsClient';
import { AuthModal } from './components/auth/AuthModal';
import { AppLayout } from './components/layout/AppLayout';
import { Sidebar } from './components/sidebar/Sidebar';
import { ChatWindow } from './components/chat/ChatWindow';
import { ProfileView } from './components/profile/ProfileView';
import { AddFriendModal } from './components/sidebar/AddFriendModal';
import { CreateGroupModal } from './components/group/CreateGroupModal';
import { GroupManagerModal } from './components/group/GroupManagerModal';
import { VoiceRecordModal } from './components/chat/VoiceRecordModal';
import { AudioCallOverlay } from './components/audio/AudioCallOverlay';
import { IncomingCallModal } from './components/audio/IncomingCallModal';
import { UserProfileModal } from './components/profile/UserProfileModal';
import { MsgType } from './proto/message';

export const App: React.FC = () => {
  const { isAuthenticated, initAuth } = useAuthStore();
  const { addIncomingMessage, updateMessageAck } = useChatStore();
  const { fetchPendingRequests, fetchFriends } = useFriendStore();

  const [activeView, setActiveView] = useState<'chat' | 'profile'>('chat');

  // 模态框状态管理
  const [isAddFriendOpen, setIsAddFriendOpen] = useState(false);
  const [addFriendPrefillUsername, setAddFriendPrefillUsername] = useState('');
  const [isCreateGroupOpen, setIsCreateGroupOpen] = useState(false);
  const [activeGroupIdForManager, setActiveGroupIdForManager] = useState<string | null>(null);
  const [isVoiceRecordOpen, setIsVoiceRecordOpen] = useState(false);
  
  // 他人主页弹窗
  const [inspectingUsername, setInspectingUsername] = useState<string | null>(null);

  useEffect(() => {
    // 1. 初始化鉴权状态
    initAuth();

    // 2. 注册 WebSocket 消息与 ACK 监听器
    const unsubMsg = wsClient.onMessage((msg) => {
      const currentUid = localStorage.getItem('neo_user_id') || '';
      addIncomingMessage(msg, currentUid);

      // 好友申请实时通知
      if (msg.type === MsgType.FRIEND_APPLY_NOTIFY) {
        fetchPendingRequests();
      } else if (msg.type === MsgType.FRIEND_ACCEPT_NOTIFY) {
        fetchFriends();
      }
    });

    const unsubAck = wsClient.onAck((stanzaId, seq) => {
      updateMessageAck(stanzaId, seq);
    });

    return () => {
      unsubMsg();
      unsubAck();
    };
  }, [initAuth, addIncomingMessage, updateMessageAck, fetchPendingRequests, fetchFriends]);

  const handleOpenAddFriend = (prefillUsername = '') => {
    setAddFriendPrefillUsername(prefillUsername);
    setIsAddFriendOpen(true);
  };

  return (
    <>
      {/* 1. 未登录时展示登录/注册模态框 */}
      {!isAuthenticated && <AuthModal />}

      {/* 2. 登录后展示主应用界面 */}
      {isAuthenticated && (
        <AppLayout activeView={activeView} setActiveView={setActiveView}>
          {activeView === 'chat' ? (
            <>
              {/* 左侧导航栏 */}
              <Sidebar
                onOpenAddFriendModal={() => handleOpenAddFriend('')}
                onOpenCreateGroupModal={() => setIsCreateGroupOpen(true)}
              />
              {/* 主聊天视窗 */}
              <ChatWindow
                onOpenVoiceRecord={() => setIsVoiceRecordOpen(true)}
                onOpenGroupManager={(gid) => setActiveGroupIdForManager(gid)}
                onOpenProfile={(username) => setInspectingUsername(username)}
              />
            </>
          ) : (
            /* 个人主页与动态流 */
            <ProfileView />
          )}

          {/* 3. 实时语音通话悬浮面板 */}
          <AudioCallOverlay />

          {/* 4. 实时来电接听/拒接弹窗 */}
          <IncomingCallModal />

          {/* 5. 他人个人主页与动态广场弹窗 */}
          <UserProfileModal
            username={inspectingUsername}
            onClose={() => setInspectingUsername(null)}
            onOpenAddFriend={(uname) => handleOpenAddFriend(uname)}
          />
        </AppLayout>
      )}

      {/* 6. 全局辅助弹窗组件 */}
      <AddFriendModal
        isOpen={isAddFriendOpen}
        onClose={() => {
          setIsAddFriendOpen(false);
          setAddFriendPrefillUsername('');
        }}
        initialUsername={addFriendPrefillUsername}
      />

      <CreateGroupModal
        isOpen={isCreateGroupOpen}
        onClose={() => setIsCreateGroupOpen(false)}
      />

      <GroupManagerModal
        groupId={activeGroupIdForManager}
        onClose={() => setActiveGroupIdForManager(null)}
      />

      <VoiceRecordModal
        isOpen={isVoiceRecordOpen}
        onClose={() => setIsVoiceRecordOpen(false)}
      />
    </>
  );
};
