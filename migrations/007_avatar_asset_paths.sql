UPDATE users
SET avatar = REPLACE(avatar, '/svg/', '/front_con/ava/')
WHERE avatar LIKE '/svg/%';

ALTER TABLE users
MODIFY COLUMN avatar VARCHAR(128) NOT NULL DEFAULT '/front_con/ava/布偶.svg';
