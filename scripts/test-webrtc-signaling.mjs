import WebSocket from 'ws';
import protobuf from 'protobufjs';

const BASE_URL = 'http://127.0.0.1:8080/api/v1';

// Protobuf 信令定义
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

const root = new protobuf.Root().add(MsgTypeEnum).add(WsMessageType);

function encodeMsg(msg) {
  return WsMessageType.encode(WsMessageType.create(msg)).finish();
}

function decodeMsg(buf) {
  return WsMessageType.decode(new Uint8Array(buf));
}

async function runWebRtcSignalingTest() {
  console.log('=== 🔍 开始 WebRTC 双向信令转发专项实机排查 ===\n');

  const ts = Date.now().toString(36);
  const userA = `rtc_a_${ts}`;
  const userB = `rtc_b_${ts}`;
  const pass = 'password123';

  // 1. 注册并登录 User A
  const regAReq = await fetch(`${BASE_URL}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: userA, password: pass }),
  });
  const regA = await regAReq.json();
  const uidA = regA.data.user_id;

  const loginAReq = await fetch(`${BASE_URL}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: userA, password: pass }),
  });
  const loginA = await loginAReq.json();
  const tokenA = loginA.data.access_token;

  // 2. 注册并登录 User B
  const regBReq = await fetch(`${BASE_URL}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: userB, password: pass }),
  });
  const regB = await regBReq.json();
  const uidB = regB.data.user_id;

  const loginBReq = await fetch(`${BASE_URL}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: userB, password: pass }),
  });
  const loginB = await loginBReq.json();
  const tokenB = loginB.data.access_token;

  console.log(`✅ 账号创建成功: A (${userA} / ${uidA}), B (${userB} / ${uidB})`);

  // 3. 建立双向好友关系 (A apply -> B accept)
  const applyReq = await fetch(`${BASE_URL}/friends/apply`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${tokenA}`,
    },
    body: JSON.stringify({ target_username: userB, remark: '测试加好友' }),
  });
  console.log('✅ A 发起申请 -> B:', applyReq.status);

  const respReq = await fetch(`${BASE_URL}/friends/respond`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${tokenB}`,
    },
    body: JSON.stringify({ target_user_id: uidA, action: 'accept' }),
  });
  console.log('✅ B 同意申请:', respReq.status);

  // 4. 获取双方 WebSocket Tickets
  const tktA = (await (await fetch(`${BASE_URL}/auth/ticket`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${tokenA}` },
  })).json()).data.ticket;

  const tktB = (await (await fetch(`${BASE_URL}/auth/ticket`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${tokenB}` },
  })).json()).data.ticket;

  console.log(`✅ 获取 Ticket: A=${tktA}, B=${tktB}`);

  // 5. 建立双端 WebSocket 连接
  const wsA = new WebSocket(`ws://127.0.0.1:8080/ws?ticket=${tktA}`);
  const wsB = new WebSocket(`ws://127.0.0.1:8080/ws?ticket=${tktB}`);

  wsA.binaryType = 'arraybuffer';
  wsB.binaryType = 'arraybuffer';

  await Promise.all([
    new Promise((resolve) => wsA.on('open', resolve)),
    new Promise((resolve) => wsB.on('open', resolve)),
  ]);
  console.log('✅ 双端 WebSocket 长连接已成功建立！\n');

  // 6. 监听双端收到的消息
  let bReceivedOffer = false;
  let aReceivedAnswer = false;

  wsB.on('message', (data) => {
    try {
      const msg = decodeMsg(data);
      console.log(`📩 [User B 收到消息] Type=${msg.type}, FromUID=${msg.from_uid}, ToUID=${msg.to_uid}, Content=${msg.content}`);
      if (msg.type === 20 /* SIGNALING_OFFER */) {
        bReceivedOffer = true;
        console.log('🎉🎉🎉 [成功] User B 成功接收到了后端转发的 SIGNALING_OFFER 信令！');

        // B 立即回复 SIGNALING_ANSWER 给 A
        console.log('➡️ [User B] 正在向 User A 回复 SIGNALING_ANSWER...');
        const answerPayload = encodeMsg({
          type: 21, // SIGNALING_ANSWER
          to_uid: uidA,
          content: JSON.stringify({ type: 'answer', sdp: 'fake-answer-sdp' }),
          stanza_id: 'stz_ans_1',
          timestamp: Date.now(),
        });
        wsB.send(answerPayload);
      }
    } catch (e) {
      console.error('B 解码错误:', e);
    }
  });

  wsA.on('message', (data) => {
    try {
      const msg = decodeMsg(data);
      console.log(`📩 [User A 收到消息] Type=${msg.type}, FromUID=${msg.from_uid}, ToUID=${msg.to_uid}, Content=${msg.content}`);
      if (msg.type === 21 /* SIGNALING_ANSWER */) {
        aReceivedAnswer = true;
        console.log('🎉🎉🎉 [成功] User A 成功接收到了后端转发的 SIGNALING_ANSWER 信令！');
      }
    } catch (e) {
      console.error('A 解码错误:', e);
    }
  });

  // 7. A 向 B 发送 SIGNALING_OFFER
  console.log('➡️ [User A] 正在向 User B 发送 SIGNALING_OFFER (Type=20)...');
  const offerPayload = encodeMsg({
    type: 20, // SIGNALING_OFFER
    to_uid: uidB,
    content: JSON.stringify({ type: 'offer', sdp: 'fake-offer-sdp' }),
    stanza_id: 'stz_off_1',
    timestamp: Date.now(),
  });
  wsA.send(offerPayload);

  // 等待 3 秒观察信令往返
  await new Promise((resolve) => setTimeout(resolve, 3000));

  console.log('\n=== 📊 测试结果汇总 ===');
  console.log('B 是否收到 Offer 信令:', bReceivedOffer ? '✅ 是' : '❌ 否');
  console.log('A 是否收到 Answer 信令:', aReceivedAnswer ? '✅ 是' : '❌ 否');

  wsA.close();
  wsB.close();
}

runWebRtcSignalingTest().catch(console.error);
