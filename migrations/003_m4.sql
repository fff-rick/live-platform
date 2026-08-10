CREATE TABLE IF NOT EXISTS gifts (
    id BIGINT PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    price BIGINT NOT NULL,
    status TINYINT NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    CHECK (price >= 0),
    KEY idx_gifts_status(status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wallets (
    user_id BIGINT PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME(3) NOT NULL,
    CHECK (balance >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wallet_transactions (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    transaction_no VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    biz_type VARCHAR(32) NOT NULL,
    biz_id VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    created_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_wallet_transactions_no(transaction_no),
    KEY idx_wallet_transactions_user_created(user_id, created_at),
    KEY idx_wallet_transactions_biz(biz_type, biz_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gift_orders (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_no VARCHAR(64) NOT NULL,
    request_id VARCHAR(128) NOT NULL,
    user_id BIGINT NOT NULL,
    anchor_id BIGINT NOT NULL,
    room_id BIGINT NOT NULL,
    gift_id BIGINT NOT NULL,
    gift_count BIGINT NOT NULL,
    unit_price BIGINT NOT NULL,
    total_amount BIGINT NOT NULL,
    status TINYINT NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_gift_orders_order_no(order_no),
    UNIQUE KEY uk_gift_orders_request_id(request_id),
    KEY idx_gift_orders_user_created(user_id, created_at),
    KEY idx_gift_orders_room_created(room_id, created_at),
    CHECK (gift_count > 0),
    CHECK (unit_price >= 0),
    CHECK (total_amount >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO gifts(id, name, price, status, created_at, updated_at) VALUES
    (1, 'Rose', 100, 1, NOW(3), NOW(3)),
    (2, 'Rocket', 10000, 1, NOW(3), NOW(3)),
    (3, 'Castle', 50000, 1, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE name=VALUES(name), price=VALUES(price), status=VALUES(status), updated_at=NOW(3);

INSERT INTO wallets(user_id, balance, version, updated_at)
SELECT id, 0, 0, NOW(3) FROM users
ON DUPLICATE KEY UPDATE user_id=user_id;
