const BASE_URL = 'http://127.0.0.1:8080/api/v1';

async function runTests() {
  console.log('=== 🚀 开始全量后端接口分块联调测试 ===\n');

  const ts = Date.now().toString(36);
  const userA = `tester_a_${ts}`;
  const userB = `tester_b_${ts}`;
  const pass = 'password123';

  let tokenA = '';
  let tokenB = '';
  let uidA = '';
  let uidB = '';
  let sidA = '';
  let ticketA = '';
  let groupId = '';

  // -------------------------------------------------------------
  // 1. 鉴权与会话模块测试 (/api/v1/auth)
  // -------------------------------------------------------------
  console.log('--- [1. 鉴权与会话模块 /api/v1/auth] ---');

  // 1.1 注册 User A
  try {
    const regAReq = await fetch(`${BASE_URL}/auth/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: userA, password: pass }),
    });
    const regA = await regAReq.json();
    console.log('✅ POST /auth/register (User A):', regAReq.status, regA);
    uidA = regA.data.user_id;
  } catch (err) {
    console.error('❌ POST /auth/register 失败:', err);
  }

  // 1.2 注册 User B
  try {
    const regBReq = await fetch(`${BASE_URL}/auth/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: userB, password: pass }),
    });
    const regB = await regBReq.json();
    console.log('✅ POST /auth/register (User B):', regBReq.status, regB);
    uidB = regB.data.user_id;
  } catch (err) {
    console.error('❌ POST /auth/register (User B) 失败:', err);
  }

  // 1.3 登录 User A
  try {
    const loginAReq = await fetch(`${BASE_URL}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: userA,
        password: pass,
        device_class: 'interactive',
        device_name: 'Test Client',
      }),
    });
    const loginA = await loginAReq.json();
    console.log('✅ POST /auth/login (User A):', loginAReq.status, loginA);
    tokenA = loginA.data.access_token;
    sidA = loginA.data.session_id;
  } catch (err) {
    console.error('❌ POST /auth/login 失败:', err);
  }

  // 1.4 登录 User B
  try {
    const loginBReq = await fetch(`${BASE_URL}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: userB,
        password: pass,
        device_class: 'interactive',
        device_name: 'Test Client',
      }),
    });
    const loginB = await loginBReq.json();
    console.log('✅ POST /auth/login (User B):', loginBReq.status, loginB);
    tokenB = loginB.data.access_token;
  } catch (err) {
    console.error('❌ POST /auth/login (User B) 失败:', err);
  }

  // 1.5 获取 WebSocket Ticket
  try {
    const tktReq = await fetch(`${BASE_URL}/auth/ticket`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${tokenA}` },
    });
    const tktRes = await tktReq.json();
    console.log('✅ POST /auth/ticket:', tktReq.status, tktRes);
    ticketA = tktRes.data.ticket;
  } catch (err) {
    console.error('❌ POST /auth/ticket 失败:', err);
  }

  // -------------------------------------------------------------
  // 2. 好友关系链模块测试 (/api/v1/friends)
  // -------------------------------------------------------------
  console.log('\n--- [2. 好友关系链模块 /api/v1/friends] ---');

  // 2.1 自己加自己 (预期 400 校验拦截)
  try {
    const selfApplyReq = await fetch(`${BASE_URL}/friends/apply`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenA}`,
      },
      body: JSON.stringify({
        target_username: userA,
        remark: '添加自己',
      }),
    });
    const selfApply = await selfApplyReq.json();
    console.log('✅ 自己加自己拦截校验 (预期 400):', selfApplyReq.status, selfApply);
  } catch (err) {
    console.error('❌ 自己加自己测试异常:', err);
  }

  // 2.2 User A 向 User B 发起申请 (带备注)
  try {
    const applyReq = await fetch(`${BASE_URL}/friends/apply`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenA}`,
      },
      body: JSON.stringify({
        target_username: userB,
        remark: '你好，我是测试用户A',
      }),
    });
    const applyRes = await applyReq.json();
    console.log('✅ POST /friends/apply (A -> B 带备注):', applyReq.status, applyRes);
  } catch (err) {
    console.error('❌ POST /friends/apply 失败:', err);
  }

  // 2.3 User B 查询待处理申请列表
  try {
    const pendingReq = await fetch(`${BASE_URL}/friends/pending`, {
      headers: { Authorization: `Bearer ${tokenB}` },
    });
    const pendingRes = await pendingReq.json();
    console.log('✅ GET /friends/pending (User B 查收申请):', pendingReq.status, JSON.stringify(pendingRes));
  } catch (err) {
    console.error('❌ GET /friends/pending 失败:', err);
  }

  // 2.4 User B 审批通过
  try {
    const respondReq = await fetch(`${BASE_URL}/friends/respond`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenB}`,
      },
      body: JSON.stringify({
        target_user_id: uidA,
        action: 'accept',
      }),
    });
    const respondRes = await respondReq.json();
    console.log('✅ POST /friends/respond (User B 同意):', respondReq.status, respondRes);
  } catch (err) {
    console.error('❌ POST /friends/respond 失败:', err);
  }

  // 2.5 查询 User A 的好友列表
  try {
    const listReq = await fetch(`${BASE_URL}/friends`, {
      headers: { Authorization: `Bearer ${tokenA}` },
    });
    const listRes = await listReq.json();
    console.log('✅ GET /friends (User A 好友列表):', listReq.status, JSON.stringify(listRes));
  } catch (err) {
    console.error('❌ GET /friends 失败:', err);
  }

  // -------------------------------------------------------------
  // 3. 群聊全生命周期模块测试 (/api/v1/groups)
  // -------------------------------------------------------------
  console.log('\n--- [3. 群聊管理模块 /api/v1/groups] ---');

  // 3.1 创建群聊
  try {
    const createGrpReq = await fetch(`${BASE_URL}/groups`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenA}`,
      },
      body: JSON.stringify({
        name: `测试研发群_${ts}`,
      }),
    });
    const createGrp = await createGrpReq.json();
    console.log('✅ POST /groups (创建群聊):', createGrpReq.status, createGrp);
    groupId = createGrp.data.id;
  } catch (err) {
    console.error('❌ POST /groups 失败:', err);
  }

  // 3.2 邀请 User B 加入群聊
  if (groupId) {
    try {
      const inviteReq = await fetch(`${BASE_URL}/groups/${groupId}/members`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${tokenA}`,
        },
        body: JSON.stringify({
          user_ids: [uidB],
        }),
      });
      const inviteRes = await inviteReq.json();
      console.log('✅ POST /groups/:id/members (邀请B入群):', inviteReq.status, inviteRes);
    } catch (err) {
      console.error('❌ POST /groups/:id/members 失败:', err);
    }

    // 3.3 查看群成员列表
    try {
      const memReq = await fetch(`${BASE_URL}/groups/${groupId}/members`, {
        headers: { Authorization: `Bearer ${tokenA}` },
      });
      const memRes = await memReq.json();
      console.log('✅ GET /groups/:id/members (群成员列表):', memReq.status, JSON.stringify(memRes));
    } catch (err) {
      console.error('❌ GET /groups/:id/members 失败:', err);
    }

    // 3.4 查看群历史消息
    try {
      const histReq = await fetch(`${BASE_URL}/groups/${groupId}/history`, {
        headers: { Authorization: `Bearer ${tokenA}` },
      });
      const histRes = await histReq.json();
      console.log('✅ GET /groups/:id/history (群历史漫游):', histReq.status, JSON.stringify(histRes));
    } catch (err) {
      console.error('❌ GET /groups/:id/history 失败:', err);
    }
  }

  // -------------------------------------------------------------
  // 4. 多媒体上传与读取模块测试 (/api/v1/media)
  // -------------------------------------------------------------
  console.log('\n--- [4. 多媒体中心模块 /api/v1/media] ---');

  let uploadedMid = '';
  try {
    const formData = new FormData();
    const fileBlob = new Blob(['fake-image-bytes-data-png'], { type: 'image/png' });
    formData.append('file', fileBlob, 'sample.png');
    formData.append('media_type', 'image');
    formData.append('access_level', 'public');

    const uploadReq = await fetch(`${BASE_URL}/media/upload`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${tokenA}`,
      },
      body: formData,
    });
    const uploadRes = await uploadReq.json();
    console.log('✅ POST /media/upload (上传图片):', uploadReq.status, uploadRes);
    uploadedMid = uploadRes.data?.mid;
  } catch (err) {
    console.error('❌ POST /media/upload 失败:', err);
  }

  // 读取已上传的多媒体文件
  if (uploadedMid) {
    try {
      const getMediaReq = await fetch(`${BASE_URL}/media/${uploadedMid}`);
      console.log('✅ GET /media/:mid (直读媒体资源):', getMediaReq.status, 'Content-Type:', getMediaReq.headers.get('content-type'));
    } catch (err) {
      console.error('❌ GET /media/:mid 失败:', err);
    }
  }

  // -------------------------------------------------------------
  // 5. 个人主页与动态流测试 (/api/v1/profile & /api/v1/posts)
  // -------------------------------------------------------------
  console.log('\n--- [5. 个人主页与动态流模块] ---');

  // 5.1 修改资料
  try {
    const updateReq = await fetch(`${BASE_URL}/profile`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenA}`,
      },
      body: JSON.stringify({
        nickname: 'Alice Wonderland',
        signature: '探索极客 IM 的奥秘',
      }),
    });
    const updateRes = await updateReq.json();
    console.log('✅ PUT /profile (修改资料):', updateReq.status, updateRes);
  } catch (err) {
    console.error('❌ PUT /profile 失败:', err);
  }

  // 5.2 发布动态 (B 站 BV 号)
  let postId = '';
  try {
    const postReq = await fetch(`${BASE_URL}/profile/posts`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${tokenA}`,
      },
      body: JSON.stringify({
        media_type: 'bilibili',
        content: '推荐一个超棒的技术视频！',
        bilibili_bvid: 'BV1xx411c7mD',
      }),
    });
    const postRes = await postReq.json();
    console.log('✅ POST /profile/posts (发布B站动态):', postReq.status, postRes);
    postId = postRes.data?.post?.id;
  } catch (err) {
    console.error('❌ POST /profile/posts 失败:', err);
  }

  // 5.3 公开查看主页
  try {
    const profileReq = await fetch(`${BASE_URL}/profile/${userA}`);
    const profileRes = await profileReq.json();
    console.log('✅ GET /profile/:username (公开查看主页):', profileReq.status, JSON.stringify(profileRes));
  } catch (err) {
    console.error('❌ GET /profile/:username 失败:', err);
  }

  // 5.4 查看动态卡片
  if (postId) {
    try {
      const cardReq = await fetch(`${BASE_URL}/posts/${postId}/card`);
      const cardRes = await cardReq.json();
      console.log('✅ GET /posts/:post_id/card (卡片跳转信息):', cardReq.status, cardRes);
    } catch (err) {
      console.error('❌ GET /posts/:post_id/card 失败:', err);
    }
  }

  console.log('\n=== 🎉 全部分块接口测试执行完毕 ===');
}

runTests().catch(console.error);
