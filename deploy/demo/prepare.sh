#!/bin/sh
# Chuẩn bị máy demo App HTX (chạy lại an toàn — không đổi bí mật đã có):
#   1. deploy/demo/.env phải có (chép từ env.example rồi sửa).
#   2. Sinh/bổ sung bí mật vào deploy/compose/.env (scripts/gen-dev-secrets.sh) + mật khẩu DB của Keycloak.
#   3. Sinh realm Keycloak cho tên miền demo vào deploy/demo/.generated/keycloak/ (gitignore).
# Cần: sh, jq, openssl (gen-dev-certs). Không in bí mật.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
demo="$root/deploy/demo"
secrets="$root/deploy/compose/.env"

[ -f "$demo/.env" ] || { echo "Thiếu $demo/.env — chép từ $demo/env.example rồi sửa" >&2; exit 1; }
PUBLIC_URL=$(sed -n 's/^PUBLIC_URL=//p' "$demo/.env" | tr -d "'\"" | sed 's:/*$::')
PUBLIC_HOST=$(sed -n 's/^PUBLIC_HOST=//p' "$demo/.env" | tr -d "'\"")
case "$PUBLIC_URL" in https://*) ;; *) echo "PUBLIC_URL phải là https://… (hiện: '$PUBLIC_URL')" >&2; exit 1 ;; esac
[ "$PUBLIC_URL" = "https://$PUBLIC_HOST" ] || { echo "PUBLIC_URL phải bằng https://\$PUBLIC_HOST" >&2; exit 1; }
for v in ACME_EMAIL ADMIN_ALLOW_CIDR; do
  [ -n "$(sed -n "s/^$v=//p" "$demo/.env")" ] || { echo "Chưa đặt $v trong $demo/.env" >&2; exit 1; }
done
command -v jq >/dev/null || { echo "Cần jq" >&2; exit 1; }

# 2. Bí mật
sh "$root/scripts/gen-dev-secrets.sh"
umask 077
for v in KEYCLOAK_DB_PASSWORD AG_SMTP_PASSWORD; do
  if ! grep -q "^$v=" "$secrets"; then
    [ -z "$(tail -c1 "$secrets")" ] || echo >> "$secrets"
    if [ "$v" = KEYCLOAK_DB_PASSWORD ]; then
      echo "$v=$(head -c 24 /dev/urandom | od -An -v -tx1 | tr -d ' \n')" >> "$secrets"
    else
      echo "$v=" >> "$secrets"   # điền tay nếu dùng SMTP
    fi
    echo "Đã bổ sung $v vào $secrets (giá trị không in ra)."
  fi
done

# 3. Realm theo tên miền: client web dùng PUBLIC_URL; tắt client không thuộc App HTX (KN/MKG/seed chỉ-dev).
out="$demo/.generated/keycloak"
mkdir -p "$out"
jq --arg url "$PUBLIC_URL" '
  .sslRequired = "external"
  | .clients |= map(
      if .clientId == "agrigreen-web-ops" then
        .redirectUris = [$url + "/*"]
        | .webOrigins = [$url]
        | .attributes["post.logout.redirect.uris"] = ($url + "/*")
      elif (.clientId | IN("agrigreen-seed", "kn-web", "agrigreen-web-internal", "agrigreen-field-app",
                           "agrigreen-mkg-field-app", "agrigreen-tms", "agrigreen-mkg-sync")) then
        .enabled = false
        | .redirectUris = []
        | .webOrigins = []
        | .attributes["post.logout.redirect.uris"] = ""
      else . end)
' "$root/deploy/compose/keycloak/agrigreen-realm.json" > "$out/agrigreen-realm.json.tmp"
mv "$out/agrigreen-realm.json.tmp" "$out/agrigreen-realm.json"
if grep -q 'localhost' "$out/agrigreen-realm.json"; then
  echo "Cảnh báo: realm sinh ra vẫn còn 'localhost' (client mới thêm vào realm gốc?) — kiểm lại:" >&2
  grep -o '"[^"]*localhost[^"]*"' "$out/agrigreen-realm.json" | sort -u >&2
fi
echo "Đã sinh $out/agrigreen-realm.json cho $PUBLIC_URL."
echo "Realm chỉ được nhập ở lần khởi động đầu; realm đã có thì sửa qua Admin Console (xem runbook)."
