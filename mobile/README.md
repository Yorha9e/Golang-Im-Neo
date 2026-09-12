# 📱 Golang IM Neo — 专属手机端移动应用 (Mobile App)

本项目为 **Golang IM Neo** 的专属移动端独立工程目录，承载移动端单手操作、触控手势、底部导航栏与多主题系统的独立演进。

---

## 🚀 快速启动

```bash
cd mobile
npm install
npm run dev
```

---

## 📦 已全量克隆与开箱即用的核心资产

| 模块目录 | 资产说明 | 移动端复用建议 |
| :--- | :--- | :--- |
| `src/api/` | 7 大分类 RESTful API 客户端（双 Token 自动无感轮换、鉴权、好友、群聊、消息漫游、动态广场、媒体上传） | **100% 直连复用**，无需重复开发网络层 |
| `src/proto/` | `message.proto` 二进制编解码器（Protobuf 定义、MsgType 枚举、StanzaID 发生器） | **100% 直连复用** |
| `src/socket/` | WebSocket 长连接管理器（Ticket 握手、心跳维持、ACK 去重重试、断线重连）与 WebRTC 语音通话引擎 | **100% 直连复用** |
| `src/store/` | Zustand 全局响应式状态库（AuthStore, ChatStore, FriendStore, AudioStore） | **100% 直连复用** |
| `src/audio/` | Web Audio API 交互音效系统（水滴声、发送声、通知音） | **100% 直连复用** |
| `src/components/` | 完整的气泡流（`MessageBubble`）、麦浪语音波形（`AudioBubble`）、多媒体大图灯箱、管理员看板、个人主页资料卡 | **按移动端设计稿直接组装与布局微调** |
| `tailwind.config.ts` | 预置 **Warm Sand（细腻暖沙与暖阳）** 调色盘与磨砂阴影滤镜 | **可在此基础扩展多主题切换（如 Dark Mode、Cyberpunk 等）** |

---

## 🎨 后续移动端重点开发任务

1. **移动端经典底部 4 栏导航栏 (Bottom Tab Bar)**：
   - 💬 **消息**（大厅/私聊/群聊会话列表）
   - 👥 **通讯录**（好友/群聊/申请审批）
   - ✨ **动态广场**（图文动态/B站卡片/大图灯箱）
   - 👤 **我的**（个人资料/换头像/音效/多主题切换）
2. **移动端触控手势系统**：
   - 左边缘右滑返回
   - 列表下拉弹性刷新
   - 聊天气泡长按快捷菜单
   - 按住麦克风全屏录音交互
3. **多主题系统（Multi-Theme）适配**：
   - 支持 Warm Sand、暗黑极夜（OLED Black）、清爽冰川等主题一键切换。
