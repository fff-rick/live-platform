import http from 'k6/http';
import ws from 'k6/ws';
import { check, fail, group, sleep } from 'k6';
import exec from 'k6/execution';
import { Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const WS_URL = __ENV.WS_URL || 'ws://localhost:8000/connection/websocket?format=json';
const DURATION = __ENV.LOAD_DURATION || '30s';
const USERS = numberEnv('LOAD_USERS', 30);

const browseRate = numberEnv('BROWSE_RATE', 50);
const heartbeatRate = numberEnv('HEARTBEAT_RATE', 5);
const danmakuRate = numberEnv('DANMAKU_RATE', 10);
const likeRate = numberEnv('LIKE_RATE', 40);
const giftRate = numberEnv('GIFT_RATE', 3);
const websocketVUs = numberEnv('WS_VUS', 20);
const websocketSessionMS = numberEnv('WS_SESSION_MS', 15000);

const browseSuccess = new Rate('business_browse_success');
const heartbeatSuccess = new Rate('business_heartbeat_success');
const danmakuSuccess = new Rate('business_danmaku_success');
const likeSuccess = new Rate('business_like_success');
const giftSuccess = new Rate('business_gift_success');
const websocketSuccess = new Rate('business_websocket_success');
const websocketSubscribeSuccess = new Rate('business_websocket_subscribe_success');

export const options = {
  setupTimeout: '2m',
  teardownTimeout: '30s',
  scenarios: {
    browse: arrivalScenario('browseRoom', browseRate, 20, 60),
    heartbeat: arrivalScenario('heartbeatRoom', heartbeatRate, 5, 20),
    danmaku: arrivalScenario('sendDanmaku', danmakuRate, 10, 40),
    like: arrivalScenario('likeRoom', likeRate, 20, 80),
    gift: arrivalScenario('sendGift', giftRate, 5, 30),
    websocket: {
      executor: 'constant-vus',
      exec: 'websocketSession',
      vus: websocketVUs,
      duration: DURATION,
      gracefulStop: '10s',
      startTime: '2s',
      tags: { workload: 'websocket' },
    },
  },
  thresholds: {
    checks: ['rate>0.99'],
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    dropped_iterations: ['count==0'],
    business_browse_success: ['rate>0.99'],
    business_heartbeat_success: ['rate>0.99'],
    business_danmaku_success: ['rate>0.99'],
    business_like_success: ['rate>0.99'],
    business_gift_success: ['rate>0.99'],
    business_websocket_success: ['rate>0.99'],
    business_websocket_subscribe_success: ['rate>0.99'],
    ws_msgs_received: ['count>0'],
  },
};

function numberEnv(name, fallback) {
  const raw = __ENV[name];
  if (raw === undefined || raw === '') return fallback;
  const value = Number(raw);
  if (!Number.isFinite(value) || value <= 0) throw new Error(`${name} must be a positive number`);
  return value;
}

function arrivalScenario(entrypoint, rate, preAllocatedVUs, maxVUs) {
  return {
    executor: 'constant-arrival-rate',
    exec: entrypoint,
    rate,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs,
    maxVUs,
    gracefulStop: '10s',
    startTime: '2s',
    tags: { workload: entrypoint },
  };
}

function authHeaders(token, extra = {}) {
  return {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...extra,
    },
  };
}

function expectStatus(response, expected, label) {
  const ok = check(response, { [`${label}: status ${expected}`]: (r) => r.status === expected });
  if (!ok) fail(`${label} failed: status=${response.status}, body=${response.body}`);
}

function register(username, nickname) {
  const response = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ username, nickname, password: 'password123' }),
    authHeaders('', { 'X-Load-Setup': 'true' }),
  );
  expectStatus(response, 201, 'register');
  return response.json();
}

// setup 只执行一次：建立一间独立直播间和用户池，避免压测数据依赖人工准备。
export function setup() {
  const ready = http.get(`${BASE_URL}/ready`, { tags: { name: 'GET /ready' } });
  expectStatus(ready, 200, 'readiness');

  const runID = `${Date.now()}_${Math.floor(Math.random() * 100000)}`;
  const anchor = register(`k6a_${runID}`, 'K6 Anchor');
  const anchorToken = anchor.access_token;
  const roomResponse = http.post(
    `${BASE_URL}/api/v1/rooms`,
    JSON.stringify({ title: 'K6 system load room' }),
    authHeaders(anchorToken),
  );
  expectStatus(roomResponse, 201, 'create room');
  const roomID = roomResponse.json('room_id');
  expectStatus(http.post(`${BASE_URL}/api/v1/rooms/${roomID}/start`, null, authHeaders(anchorToken)), 200, 'start room');

  const giftList = http.get(`${BASE_URL}/api/v1/gifts`, { tags: { name: 'GET /gifts (setup)' } });
  expectStatus(giftList, 200, 'gift list');
  const giftID = giftList.json('items.0.gift_id');

  const viewers = [];
  for (let i = 0; i < USERS; i += 1) {
    const viewer = register(`k6v_${runID}_${i}`, `K6 Viewer ${i}`);
    expectStatus(
      http.post(
        `${BASE_URL}/api/v1/wallet/dev-credit`,
        JSON.stringify({ amount: 100000000 }),
        authHeaders(viewer.access_token),
      ),
      200,
      'credit wallet',
    );
    viewers.push({ id: viewer.user.user_id || viewer.user.id, token: viewer.access_token });
  }

  // 治理链路执行写入、查询、撤销；撤销后再签发实时令牌，确保负载全部是合法请求。
  const target = viewers[0];
  expectStatus(
    http.post(
      `${BASE_URL}/api/v1/rooms/${roomID}/mutes`,
      JSON.stringify({ user_id: target.id, duration_seconds: 30, reason: 'k6 setup validation' }),
      authHeaders(anchorToken),
    ),
    200,
    'mute user',
  );
  expectStatus(http.get(`${BASE_URL}/api/v1/rooms/${roomID}/mutes`, authHeaders(anchorToken)), 200, 'list mutes');
  expectStatus(http.del(`${BASE_URL}/api/v1/rooms/${roomID}/mutes/${target.id}`, null, authHeaders(anchorToken)), 200, 'unmute user');
  expectStatus(
    http.post(
      `${BASE_URL}/api/v1/rooms/${roomID}/bans`,
      JSON.stringify({ user_id: target.id, reason: 'k6 setup validation' }),
      authHeaders(anchorToken),
    ),
    200,
    'ban user',
  );
  expectStatus(http.get(`${BASE_URL}/api/v1/rooms/${roomID}/bans`, authHeaders(anchorToken)), 200, 'list bans');
  expectStatus(http.del(`${BASE_URL}/api/v1/rooms/${roomID}/bans/${target.id}`, null, authHeaders(anchorToken)), 200, 'unban user');

  for (const viewer of viewers) {
    const join = http.post(`${BASE_URL}/api/v1/rooms/${roomID}/join`, null, authHeaders(viewer.token));
    expectStatus(join, 200, 'join room');
    const realtime = http.post(`${BASE_URL}/api/v1/realtime/token`, null, authHeaders(viewer.token));
    expectStatus(realtime, 200, 'realtime token');
    const stream = join.json('subscriptions').find((item) => item.channel === `room:${roomID}:stream`);
    if (!stream) fail('room stream subscription token is missing');
    viewer.connectionToken = realtime.json('token');
    viewer.streamChannel = stream.channel;
    viewer.subscriptionToken = stream.token;
  }

  return { runID, roomID, giftID, viewers };
}

function viewerFor(data) {
  return data.viewers[exec.scenario.iterationInTest % data.viewers.length];
}

export function browseRoom(data) {
  let ok = true;
  group('browse living rooms and stats', () => {
    const rooms = http.get(`${BASE_URL}/api/v1/rooms?status=LIVING&limit=24`, { tags: { name: 'GET /rooms' } });
    const stats = http.get(`${BASE_URL}/api/v1/rooms/${data.roomID}/stats`, { tags: { name: 'GET /rooms/:id/stats' } });
    ok = check(rooms, { 'room list status 200': (r) => r.status === 200 }) && ok;
    ok = check(stats, { 'room stats status 200': (r) => r.status === 200 }) && ok;
  });
  browseSuccess.add(ok);
}

export function heartbeatRoom(data) {
  const viewer = viewerFor(data);
  const response = http.post(
    `${BASE_URL}/api/v1/rooms/${data.roomID}/heartbeat`,
    null,
    { ...authHeaders(viewer.token), tags: { name: 'POST /rooms/:id/heartbeat' } },
  );
  heartbeatSuccess.add(check(response, { 'heartbeat status 200': (r) => r.status === 200 }));
}

export function sendDanmaku(data) {
  const viewer = viewerFor(data);
  const response = http.post(
    `${BASE_URL}/api/v1/rooms/${data.roomID}/danmaku`,
    JSON.stringify({ content: `k6 danmaku ${exec.scenario.iterationInTest}` }),
    { ...authHeaders(viewer.token), tags: { name: 'POST /rooms/:id/danmaku' } },
  );
  danmakuSuccess.add(check(response, { 'danmaku status 200': (r) => r.status === 200 }));
}

export function likeRoom(data) {
  const viewer = viewerFor(data);
  const response = http.post(
    `${BASE_URL}/api/v1/rooms/${data.roomID}/like`,
    JSON.stringify({ count: 1 }),
    { ...authHeaders(viewer.token), tags: { name: 'POST /rooms/:id/like' } },
  );
  likeSuccess.add(check(response, { 'like status 202': (r) => r.status === 202 }));
}

export function sendGift(data) {
  const viewer = viewerFor(data);
  const requestID = `k6-${data.runID}-${exec.scenario.iterationInTest}-${__VU}`;
  const response = http.post(
    `${BASE_URL}/api/v1/rooms/${data.roomID}/gifts`,
    JSON.stringify({ gift_id: data.giftID, count: 1 }),
    {
      ...authHeaders(viewer.token, { 'Idempotency-Key': requestID }),
      tags: { name: 'POST /rooms/:id/gifts' },
    },
  );
  giftSuccess.add(check(response, { 'gift status 200': (r) => r.status === 200 }));
}

export function websocketSession(data) {
  const viewer = viewerFor(data);
  let subscribed = false;
  const response = ws.connect(WS_URL, { tags: { name: 'WS /connection/websocket' } }, (socket) => {
    socket.on('open', () => {
      socket.send(JSON.stringify({ id: 1, connect: { token: viewer.connectionToken } }));
    });
    socket.on('message', (raw) => {
      for (const line of String(raw).trim().split('\n')) {
        if (!line) continue;
        const message = JSON.parse(line);
        if (message.id === 1 && message.connect) {
          socket.send(JSON.stringify({
            id: 2,
            subscribe: { channel: viewer.streamChannel, token: viewer.subscriptionToken },
          }));
        }
        if (message.id === 2 && message.subscribe && !subscribed) {
          subscribed = true;
          websocketSubscribeSuccess.add(true);
        }
      }
    });
    socket.on('error', () => websocketSuccess.add(false));
    socket.setTimeout(() => {
      if (!subscribed) websocketSubscribeSuccess.add(false);
      socket.close();
    }, websocketSessionMS);
  });
  websocketSuccess.add(check(response, { 'websocket upgraded': (r) => r && r.status === 101 }));
  sleep(0.1);
}

export function teardown(data) {
  const anchorLogin = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ username: `k6a_${data.runID}`, password: 'password123' }),
    authHeaders(''),
  );
  if (anchorLogin.status === 200) {
    http.post(`${BASE_URL}/api/v1/rooms/${data.roomID}/stop`, null, authHeaders(anchorLogin.json('access_token')));
  }
}
