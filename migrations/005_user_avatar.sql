ALTER TABLE users ADD COLUMN avatar VARCHAR(128) NOT NULL DEFAULT '/svg/布偶.svg' AFTER nickname;
UPDATE users SET avatar = '/svg/布偶.svg' WHERE avatar = '';
