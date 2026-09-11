import assert from 'node:assert/strict';
import test from 'node:test';

import {giftMessageID, rememberMessage} from './message-dedup.mjs';

test('历史与实时礼物按同一个业务 message_id 去重', () => {
  const seen = new Set();
  assert.equal(rememberMessage(seen, 'gift:G001'), true);

  const realtime = {event_id: 'evt-789', message_id: 'gift:G001', order_no: 'G001'};
  assert.equal(rememberMessage(seen, giftMessageID(realtime)), false);
});

test('旧实时载荷可从 order_no 推导业务 message_id', () => {
  assert.equal(giftMessageID({order_no: 'G002'}), 'gift:G002');
});

test('不同订单不会因为礼物参数相同而合并', () => {
  const seen = new Set();
  assert.equal(rememberMessage(seen, giftMessageID({order_no: 'G003'})), true);
  assert.equal(rememberMessage(seen, giftMessageID({order_no: 'G004'})), true);
});
