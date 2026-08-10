CREATE TABLE IF NOT EXISTS users (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(32) NOT NULL,
    nickname VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_users_username(username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS live_rooms (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    anchor_id BIGINT NOT NULL,
    title VARCHAR(100) NOT NULL,
    status VARCHAR(16) NOT NULL,
    started_at DATETIME(3) NULL,
    ended_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    KEY idx_live_rooms_anchor(anchor_id),
    KEY idx_live_rooms_status(status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS live_sessions (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    joined_at DATETIME(3) NOT NULL,
    last_seen_at DATETIME(3) NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    UNIQUE KEY uk_live_sessions_room_user(room_id, user_id),
    KEY idx_live_sessions_room_status(room_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS room_mutes (
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    muted_until DATETIME(3) NULL,
    created_by BIGINT NOT NULL,
    reason VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY(room_id, user_id),
    KEY idx_room_mutes_until(muted_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS room_bans (
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL,
    reason VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY(room_id, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
