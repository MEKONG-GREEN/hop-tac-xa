#!/bin/sh
# Gọi docker compose với đúng bộ tệp của máy demo App HTX. Mọi tham số chuyển nguyên cho docker compose.
#   sh deploy/demo/compose.sh up -d --build --wait
#   sh deploy/demo/compose.sh ps
#   sh deploy/demo/compose.sh logs -f api-platform
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
for f in "$root/deploy/compose/.env" "$root/deploy/demo/.env"; do
  [ -f "$f" ] || { echo "Thiếu $f — chạy sh deploy/demo/prepare.sh trước" >&2; exit 1; }
done

exec docker compose \
  --env-file "$root/deploy/compose/.env" \
  --env-file "$root/deploy/demo/.env" \
  -f "$root/deploy/compose/docker-compose.yml" \
  -f "$root/deploy/demo/docker-compose.demo.yml" \
  "$@"
