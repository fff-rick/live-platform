#!/usr/bin/env sh
set -eu

mysql_container="$(docker compose ps -q mysql)"
if [ -z "$mysql_container" ]; then
  echo "MySQL Compose container is not running" >&2
  exit 1
fi

expect_denied() {
  user="$1"
  password="$2"
  sql="$3"
  if docker exec "$mysql_container" mysql -u"$user" -p"$password" -N -e "$sql" >/dev/null 2>&1; then
    echo "unexpected cross-schema access for $user: $sql" >&2
    exit 1
  fi
}

expect_denied live_commerce_app commerce-app 'SELECT COUNT(*) FROM room.live_rooms'
expect_denied live_room_app room-app 'SELECT COUNT(*) FROM identity.users'
expect_denied live_identity_app identity-app 'SELECT COUNT(*) FROM room.live_rooms'
expect_denied live_query_audit query-audit 'SELECT COUNT(*) FROM identity.users'

docker exec "$mysql_container" mysql -ulive_query_audit -pquery-audit -N query \
  -e 'SELECT COUNT(*) FROM user_display_projection; SELECT COUNT(*) FROM room_message_feed;' >/dev/null

echo "P2 database boundaries: PASS"
