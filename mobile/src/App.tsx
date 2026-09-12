import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { useAuthStore, useChatStore, useFriendStore, Conversation } from './store';
import { wsClient } from './socket/wsClient';
import { MsgType } from './proto/message';

// 认证与模态框
import { AuthModal } from './components/auth/AuthModal';
import { AddFriendModal } from './components/sidebar/AddFriendModal';
import { CreateGroupModal } from './components/group/CreateGroupModal';
import { GroupManagerModal } from './components/group/GroupManagerModal';
import { VoiceRecordModal } from './components/chat/VoiceRecordModal';
import { AudioCallOverlay } from './components/audio/AudioCallOverlay';
import { IncomingCallModal } from './components/audio/IncomingCallModal';
import { UserProfileModal } from './components/profile/UserProfileModal';
import { AdminDashboardModal } from './components/admin/AdminDashboardModal';
import { GlobalBroadcastBanner } from './components/common/GlobalBroadcastBanner';
import { SystemNoticeDrawer } from './components/common/SystemNoticeDrawer';

// Neo 移动端全新核心视图
import { OrbitalGravityHub } from './components/orbital/OrbitalGravityHub';
import { ContactsGalaxyView } from './components/contacts/ContactsGalaxyView';
import { TidalBentoView } from './components/tidal/TidalBentoView';
import { ProfileView } from './components/profile/ProfileView';
import { MorphingChatDeck } from './components/chat/MorphingChatDeck';
import { ThumbDynamicIsland, IslandTabType } from './components/navigation/ThumbDynamicIsland';

export const App: React.FC = () => {
  const { isAuthenticated, initAuth } = useAuthStore();
  const {
    conversations,
    setActiveConversation,
    addIncomingMessage,
    updateMessageAck,
    markNoticesAsRead,
  } = useChatStore();
  const {
    pendingRequests,
    fetchPendingRequests,
    fetchFriends,
    fetchAll,
  } = useFriendStore();

  // Neo 移动端导航与会话状态
  const [activeTab, setActiveTab] = useState<IslandTabType>('orbital');
  const [isChatDeckOpen, setIsChatDeckOpen] = useState(false);
  const [isCreatePostOpen, setIsCreatePostOpen] = useState(false);

  // 模态框状态
  const [isAddFriendOpen, setIsAddFriendOpen] = useState(false);
  const [addFriendPrefillUsername, setAddFriendPrefillUsername] = useState('');
  const [isCreateGroupOpen, setIsCreateGroupOpen] = useState(false);
  const [activeGroupIdForManager, setActiveGroupIdForManager] = useState<string | null>(null);
  const [isVoiceRecordOpen, setIsVoiceRecordOpen] = useState(false);
  const [inspectingUsername, setInspectingUsername] = useState<string | null>(null);
  const [isAdminDashboardOpen, setIsAdminDashboardOpen] = useState(false);
  const [isNoticeDrawerOpen, setIsNoticeDrawerOpen] = useState(false);

  useEffect(() => {
    // 1. 初始化用户登录状态
    initAuth();

    // 2. 注册长连接消息分发与 ACK 回执监听
    const unsubMsg = wsClient.onMessage((msg) => {
      const currentUid = localStorage.getItem('neo_user_id') || '';
      addIncomingMessage(msg, currentUid);

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

  // 当登录成功后自动拉取一次全局好友与群组
  useEffect(() => {
    if (isAuthenticated) {
      fetchAll();
    }
  }, [isAuthenticated, fetchAll]);

  // 打开会话聊天叠层
  const handleEnterChat = (conv: Conversation) => {
    setActiveConversation(conv);
    setIsChatDeckOpen(true);
  };

  // 快捷打开添加好友
  const handleOpenAddFriend = (prefillUsername = '') => {
    setAddFriendPrefillUsername(prefillUsername);
    setIsAddFriendOpen(true);
  };

  // 统计未读消息总数
  const totalUnreadCount = conversations.reduce(
    (acc, curr) => acc + (curr.unreadCount || 0),
    0
  );

  return (
    <div className="w-full h-full min-h-screen bg-[#FAF8F5] text-[#2E2419] font-sans overflow-hidden flex flex-col relative">
      {/* 背景柔和暖沙微光渐变层 */}
      <div className="fixed inset-0 pointer-events-none bg-[radial-gradient(ellipse_at_top,_var(--tw-gradient-stops))] from-amber-100/30 via-transparent to-transparent -z-10" />

      {/* 1. 未登录时居中展示暖沙质感登录/注册卡片 */}
      {!isAuthenticated && <AuthModal />}

      {/* 2. 已登录主视口 (按 Tab 切换) */}
      {isAuthenticated && (
        <main className="flex-1 w-full h-full relative overflow-hidden flex flex-col">
          <AnimatePresence mode="wait">
            {activeTab === 'orbital' && (
              <motion.div
                key="orbital"
                initial={{ opacity: 0, scale: 0.98 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98 }}
                transition={{ duration: 0.15 }}
                className="w-full h-full"
              >
                <OrbitalGravityHub
                  onEnterChat={handleEnterChat}
                  onOpenProfile={(uname) => setInspectingUsername(uname)}
                />
              </motion.div>
            )}

            {activeTab === 'contacts' && (
              <motion.div
                key="contacts"
                initial={{ opacity: 0, scale: 0.98 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98 }}
                transition={{ duration: 0.15 }}
                className="w-full h-full"
              >
                <ContactsGalaxyView
                  onEnterChat={handleEnterChat}
                  onOpenProfile={(uname) => setInspectingUsername(uname)}
                  onOpenAddFriend={() => handleOpenAddFriend('')}
                  onOpenCreateGroup={() => setIsCreateGroupOpen(true)}
                />
              </motion.div>
            )}

            {activeTab === 'tidal' && (
              <motion.div
                key="tidal"
                initial={{ opacity: 0, scale: 0.98 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98 }}
                transition={{ duration: 0.15 }}
                className="w-full h-full"
              >
                <TidalBentoView
                  onOpenProfile={(uname) => setInspectingUsername(uname)}
                  onEnterChat={handleEnterChat}
                  isCreateOpen={isCreatePostOpen}
                  onCloseCreate={() => setIsCreatePostOpen(false)}
                />
              </motion.div>
            )}

            {activeTab === 'core' && (
              <motion.div
                key="core"
                initial={{ opacity: 0, scale: 0.98 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98 }}
                transition={{ duration: 0.15 }}
                className="w-full h-full"
              >
                <ProfileView />
              </motion.div>
            )}
          </AnimatePresence>
        </main>
      )}

      {/* 3. 聊天窗口叠层流体卡片 (MorphingChatDeck，支持向右拖拽顺滑返回) */}
      <AnimatePresence>
        {isAuthenticated && isChatDeckOpen && (
          <MorphingChatDeck
            onBack={() => setIsChatDeckOpen(false)}
            onOpenVoiceRecord={() => setIsVoiceRecordOpen(true)}
            onOpenGroupManager={(gid) => setActiveGroupIdForManager(gid)}
            onOpenProfile={(uname) => setInspectingUsername(uname)}
            onSwitchConversation={(conv) => setActiveConversation(conv)}
          />
        )}
      </AnimatePresence>

      {/* 4. 底部灵动大拇指跑道胶囊导航 (ThumbDynamicIsland) */}
      {isAuthenticated && !isChatDeckOpen && (
        <ThumbDynamicIsland
          activeTab={activeTab}
          onSelectTab={(tab) => setActiveTab(tab)}
          unreadCount={totalUnreadCount}
          pendingRequestCount={pendingRequests.length}
          onOpenAddFriend={() => handleOpenAddFriend('')}
          onOpenCreateGroup={() => setIsCreateGroupOpen(true)}
          onOpenNoticeDrawer={() => {
            markNoticesAsRead();
            setIsNoticeDrawerOpen(true);
          }}
          onOpenCreatePost={() => {
            setActiveTab('tidal');
            setIsCreatePostOpen(true);
          }}
        />
      )}

      {/* 5. 全服顶部悬浮公告横幅 */}
      <GlobalBroadcastBanner />

      {/* 6. 实时 WebRTC 音视频与电话浮层 */}
      <AudioCallOverlay />
      <IncomingCallModal />

      {/* 7. 他人主页与公开动态查看弹窗 */}
      <UserProfileModal
        username={inspectingUsername}
        onClose={() => setInspectingUsername(null)}
        onOpenAddFriend={(uname) => handleOpenAddFriend(uname)}
      />

      {/* 8. 管理员运维监控看板 */}
      <AdminDashboardModal
        isOpen={isAdminDashboardOpen}
        onClose={() => setIsAdminDashboardOpen(false)}
      />

      {/* 9. 系统历史公告抽屉 */}
      <SystemNoticeDrawer
        isOpen={isNoticeDrawerOpen}
        onClose={() => setIsNoticeDrawerOpen(false)}
      />

      {/* 10. 辅助表单弹窗 */}
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
    </div>
  );
};
