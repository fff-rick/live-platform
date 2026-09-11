import assert from 'node:assert/strict';
import test from 'node:test';

import {GiftRequestStore, isDefinitiveGiftError} from './gift-request.mjs';

class MemoryStorage {
  constructor() { this.values = new Map(); }
  getItem(key) { return this.values.get(key) ?? null; }
  setItem(key, value) { this.values.set(key, value); }
  removeItem(key) { this.values.delete(key); }
}

test('an unresolved gift intent reuses its request ID after reload', () => {
  const storage = new MemoryStorage();
  let sequence = 0;
  const intent = {roomID: 7, giftID: 9, count: 3};
  const firstStore = new GiftRequestStore(storage, () => `request-${++sequence}`);
  const first = firstStore.getOrCreate(42, intent);
  const reloadedStore = new GiftRequestStore(storage, () => `request-${++sequence}`);
  const retry = reloadedStore.getOrCreate(42, intent);

  assert.deepEqual(first, {requestID: 'request-1', roomID: 7, giftID: 9, count: 3});
  assert.equal(retry.requestID, first.requestID);
  assert.equal(sequence, 1);
});

test('a completed intent receives a new request ID next time', () => {
  const storage = new MemoryStorage();
  let sequence = 0;
  const store = new GiftRequestStore(storage, () => `request-${++sequence}`);
  const intent = {roomID: 7, giftID: 9, count: 3};
  const first = store.getOrCreate(42, intent);

  store.complete(42, intent, first.requestID);
  const next = store.getOrCreate(42, intent);

  assert.equal(next.requestID, 'request-2');
});

test('an old response cannot clear a newer request', () => {
  const storage = new MemoryStorage();
  let sequence = 0;
  const store = new GiftRequestStore(storage, () => `request-${++sequence}`);
  const intent = {roomID: 7, giftID: 9, count: 3};
  const first = store.getOrCreate(42, intent);
  store.complete(42, intent, first.requestID);
  const second = store.getOrCreate(42, intent);

  store.complete(42, intent, first.requestID);

  assert.equal(store.getOrCreate(42, intent).requestID, second.requestID);
});

test('only definitive HTTP failures clear an intent', () => {
  assert.equal(isDefinitiveGiftError({status: 400}), true);
  assert.equal(isDefinitiveGiftError({status: 409}), true);
  assert.equal(isDefinitiveGiftError({status: 408}), false);
  assert.equal(isDefinitiveGiftError({status: 500}), false);
  assert.equal(isDefinitiveGiftError(new TypeError('network failed')), false);
});

test('the request is not returned when it cannot be persisted', () => {
  const storage = new MemoryStorage();
  storage.setItem = () => { throw new Error('quota exceeded'); };
  const store = new GiftRequestStore(storage, () => 'request-1');

  assert.throws(
    () => store.getOrCreate(42, {roomID: 7, giftID: 9, count: 3}),
    error => error.code === 'GIFT_REQUEST_STORAGE',
  );
});
