const DEFAULT_LIMIT = 5000;

// 礼物的展示身份属于订单；缺省回退可兼容滚动发布期间尚未携带 message_id 的实时消息。
export function giftMessageID(event = {}) {
  const explicit = typeof event.message_id === 'string' ? event.message_id.trim() : '';
  if (explicit) return explicit;
  const orderNo = typeof event.order_no === 'string' ? event.order_no.trim() : '';
  return orderNo ? `gift:${orderNo}` : '';
}

export function rememberMessage(seen, messageID, limit = DEFAULT_LIMIT) {
  if (!messageID || seen.has(messageID)) return false;
  seen.add(messageID);
  if (seen.size > limit) seen.delete(seen.values().next().value);
  return true;
}
