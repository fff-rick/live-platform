CREATE TABLE IF NOT EXISTS room_like_snapshots (
    room_id BIGINT PRIMARY KEY,
    like_count BIGINT NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    CHECK (like_count >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
