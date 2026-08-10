CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    event_id VARCHAR(64) NOT NULL,
    aggregate_type VARCHAR(32) NOT NULL,
    aggregate_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    topic VARCHAR(128) NOT NULL,
    partition_key VARCHAR(128) NOT NULL,
    payload JSON NOT NULL,
    status TINYINT NOT NULL DEFAULT 0,
    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at DATETIME(3) NULL,
    locked_by VARCHAR(64) NULL,
    locked_at DATETIME(3) NULL,
    last_error VARCHAR(512) NULL,
    created_at DATETIME(3) NOT NULL,
    published_at DATETIME(3) NULL,
    UNIQUE KEY uk_outbox_event_id(event_id),
    KEY idx_outbox_dispatch(status, next_retry_at, id),
    KEY idx_outbox_lock(status, locked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS processed_events (
    consumer_group VARCHAR(128) NOT NULL,
    event_id VARCHAR(64) NOT NULL,
    status TINYINT NOT NULL DEFAULT 0,
    locked_at DATETIME(3) NULL,
    processed_at DATETIME(3) NULL,
    last_error VARCHAR(512) NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (consumer_group, event_id),
    KEY idx_processed_status(status, locked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS danmaku_records (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    message_id VARCHAR(64) NOT NULL,
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    nickname VARCHAR(64) NOT NULL,
    content VARCHAR(512) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_danmaku_message_id(message_id),
    KEY idx_danmaku_room_created(room_id, created_at),
    KEY idx_danmaku_user_created(user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
