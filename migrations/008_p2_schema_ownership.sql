CREATE DATABASE IF NOT EXISTS identity CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS room CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS commerce CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS archive CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS query CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS identity.users LIKE live.users;
INSERT IGNORE INTO identity.users SELECT * FROM live.users;

CREATE TABLE IF NOT EXISTS room.live_rooms LIKE live.live_rooms;
INSERT IGNORE INTO room.live_rooms SELECT * FROM live.live_rooms;
CREATE TABLE IF NOT EXISTS room.live_sessions LIKE live.live_sessions;
INSERT IGNORE INTO room.live_sessions SELECT * FROM live.live_sessions;
CREATE TABLE IF NOT EXISTS room.room_mutes LIKE live.room_mutes;
INSERT IGNORE INTO room.room_mutes SELECT * FROM live.room_mutes;
CREATE TABLE IF NOT EXISTS room.room_bans LIKE live.room_bans;
INSERT IGNORE INTO room.room_bans SELECT * FROM live.room_bans;

CREATE TABLE IF NOT EXISTS commerce.gifts LIKE live.gifts;
INSERT IGNORE INTO commerce.gifts SELECT * FROM live.gifts;
CREATE TABLE IF NOT EXISTS commerce.wallets LIKE live.wallets;
INSERT IGNORE INTO commerce.wallets SELECT * FROM live.wallets;
CREATE TABLE IF NOT EXISTS commerce.wallet_transactions LIKE live.wallet_transactions;
INSERT IGNORE INTO commerce.wallet_transactions SELECT * FROM live.wallet_transactions;
CREATE TABLE IF NOT EXISTS commerce.gift_orders LIKE live.gift_orders;
INSERT IGNORE INTO commerce.gift_orders SELECT * FROM live.gift_orders;
CREATE TABLE IF NOT EXISTS commerce.outbox_events LIKE live.outbox_events;
INSERT IGNORE INTO commerce.outbox_events
SELECT * FROM live.outbox_events WHERE aggregate_type='GIFT_ORDER';

CREATE TABLE IF NOT EXISTS archive.danmaku_records LIKE live.danmaku_records;
INSERT IGNORE INTO archive.danmaku_records SELECT * FROM live.danmaku_records;
CREATE TABLE IF NOT EXISTS archive.processed_events LIKE live.processed_events;
INSERT IGNORE INTO archive.processed_events SELECT * FROM live.processed_events;

CREATE TABLE IF NOT EXISTS query.room_like_snapshots LIKE live.room_like_snapshots;
INSERT IGNORE INTO query.room_like_snapshots SELECT * FROM live.room_like_snapshots;

CREATE TABLE IF NOT EXISTS identity.identity_outbox_events LIKE live.outbox_events;
CREATE TABLE IF NOT EXISTS room.room_outbox_events LIKE live.outbox_events;

-- shortcut: these security-definer views remove source-schema grants from live-query;
-- replace them with Kafka-maintained tables when the P3 projection pipeline lands.
CREATE OR REPLACE SQL SECURITY DEFINER VIEW query.user_display_projection AS
SELECT id AS user_id, nickname, avatar, status, created_at, updated_at
FROM identity.users;

CREATE OR REPLACE SQL SECURITY DEFINER VIEW query.room_message_feed AS
SELECT 'danmaku' AS type, d.message_id, d.room_id, d.user_id, d.nickname,
       d.content, '' AS gift_name, 0 AS gift_count, d.created_at, d.id AS sequence
FROM archive.danmaku_records d
UNION ALL
SELECT 'gift' AS type, CONCAT('gift:', o.order_no) AS message_id, o.room_id,
       o.user_id, u.nickname, '' AS content, g.name AS gift_name,
       o.gift_count, o.created_at, o.id AS sequence
FROM commerce.gift_orders o
JOIN identity.users u ON u.id=o.user_id
JOIN commerce.gifts g ON g.id=o.gift_id
WHERE o.status=1;
