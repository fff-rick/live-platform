const STORAGE_PREFIX = 'live_pending_gift_request_v1';

function positiveInteger(value, name) {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error(`${name} must be a positive integer`);
  return parsed;
}

function persistenceError(cause) {
  const error = new Error('无法保存送礼请求，请检查浏览器存储设置');
  error.code = 'GIFT_REQUEST_STORAGE';
  error.cause = cause;
  return error;
}

export class GiftRequestStore {
  constructor(storage, idFactory) {
    this.storage = storage;
    this.idFactory = idFactory;
  }

  storageKey(userID, intent) {
    const user = positiveInteger(userID, 'userID');
    const roomID = positiveInteger(intent.roomID, 'roomID');
    const giftID = positiveInteger(intent.giftID, 'giftID');
    const count = positiveInteger(intent.count, 'count');
    return `${STORAGE_PREFIX}:${user}:${roomID}:${giftID}:${count}`;
  }

  getOrCreate(userID, intent) {
    const normalized = {
      roomID: positiveInteger(intent.roomID, 'roomID'),
      giftID: positiveInteger(intent.giftID, 'giftID'),
      count: positiveInteger(intent.count, 'count'),
    };
    const key = this.storageKey(userID, normalized);
    let raw;
    try {
      raw = this.storage.getItem(key);
    } catch (error) {
      throw persistenceError(error);
    }
    if (raw) {
      try {
        const pending = JSON.parse(raw);
        if (typeof pending.requestID === 'string' && pending.requestID &&
            pending.roomID === normalized.roomID && pending.giftID === normalized.giftID && pending.count === normalized.count) {
          return pending;
        }
      } catch {
        // 损坏的本地记录不能用于重试，下面会用新请求覆盖它。
      }
    }
    const pending = {requestID: this.idFactory(), ...normalized};
    try {
      // 必须先持久化再发送，保证响应丢失后仍能取得同一个 requestID。
      this.storage.setItem(key, JSON.stringify(pending));
    } catch (error) {
      throw persistenceError(error);
    }
    return pending;
  }

  complete(userID, intent, requestID) {
    const key = this.storageKey(userID, intent);
    try {
      const raw = this.storage.getItem(key);
      if (!raw) return;
      const pending = JSON.parse(raw);
      // 防止较早请求的响应删除同参数下后来创建的新请求。
      if (pending.requestID === requestID) this.storage.removeItem(key);
    } catch {
      // 删除失败只会导致后续安全重放，不能把已经成功的扣款报告成失败。
    }
  }
}

export function isDefinitiveGiftError(error) {
  const status = Number(error?.status);
  return Number.isInteger(status) && status >= 400 && status < 500 && status !== 408;
}
