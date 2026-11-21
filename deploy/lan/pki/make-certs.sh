#!/usr/bin/env bash
# Makes the LAN stack's test PKI in deploy/lan/pki/out (not in git):
#   ca.crt / ca.key        a self-signed test CA ("OMS LAN Test CA"), 10 years
#   server.crt / .key      HTTPS for OMS and Keycloak: the LAN host, localhost,
#                          and the compose service names, signed by the CA
#   users/<name>.p12       a client certificate per user for signing in to
#                          Keycloak with a certificate; its email (in the
#                          subject and SAN) is how Keycloak finds the account
#   secrets.env            Keycloak admin password, the app's client secret
#                          and each user's password, all random
#   users.txt              who is who, with passwords, for you to read
#
# Usage: make-certs.sh <lan-host>   e.g. make-certs.sh jr-pc1.jrmagta.home
# Safe to run again: existing files are kept, so certificates you have
# already imported stay valid. Delete out/ to start over.
set -euo pipefail

host=${1:?usage: make-certs.sh <lan-host>}
cd "$(dirname "$0")"
out=out
mkdir -p "$out/users"
chmod 700 "$out"

# name|First|Last|email — the LAN realm's people. jonathan is the owner's own account.
users=(
  "jonathan|Jonathan|Magtalas|jonathan@jrmagta.home"
  "alice|Alice|Example|alice@example.com"
  "bob|Bob|Example|bob@example.com"
  "carol|Carol|Directory|carol@example.com"
)

rand() { openssl rand -base64 18 | tr -d '/+=' | cut -c1-20; }

if [[ ! -f $out/ca.key ]]; then
  openssl req -x509 -newkey rsa:3072 -nodes -days 3650 -sha256 \
    -subj "/CN=OMS LAN Test CA/O=oh-my-story LAN" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "$out/ca.key" -out "$out/ca.crt" 2>/dev/null
  echo "made the test CA"
fi

if [[ ! -f $out/server.key || ! -f $out/server.host || $(cat "$out/server.host") != "$host" ]]; then
  openssl req -newkey rsa:2048 -nodes -subj "/CN=$host" -keyout "$out/server.key" -out "$out/server.csr" 2>/dev/null
  openssl x509 -req -in "$out/server.csr" -CA "$out/ca.crt" -CAkey "$out/ca.key" -CAcreateserial -days 825 -sha256 \
    -extfile <(printf '%s\n' \
      "subjectAltName=DNS:$host,DNS:localhost,DNS:keycloak,DNS:proxy,IP:127.0.0.1" \
      "basicConstraints=critical,CA:FALSE" \
      "keyUsage=critical,digitalSignature,keyEncipherment" \
      "extendedKeyUsage=serverAuth") \
    -out "$out/server.crt" 2>/dev/null
  rm -f "$out/server.csr"
  echo "$host" > "$out/server.host"
  echo "made the server certificate for $host"
fi

touch "$out/secrets.env"
secret() { # secret NAME: print the saved value, making one the first time
  local v
  v=$(grep -E "^$1=" "$out/secrets.env" | cut -d= -f2- || true)
  if [[ -z $v ]]; then v=$(rand); echo "$1=$v" >> "$out/secrets.env"; fi
  printf '%s' "$v"
}
secret KC_BOOTSTRAP_ADMIN_PASSWORD >/dev/null
secret OMS_OIDC_CLIENT_SECRET >/dev/null

{
  echo "# LAN Keycloak people. Passwords also work; certificates are in users/<name>.p12"
  echo "# Keycloak admin console: https://$host:8444/admin  admin / $(secret KC_BOOTSTRAP_ADMIN_PASSWORD)"
  echo
} > "$out/users.txt"
for u in "${users[@]}"; do
  IFS='|' read -r name first last email <<<"$u"
  up=$(echo "$name" | tr '[:lower:]' '[:upper:]')
  pw=$(secret "LAN_PASSWORD_$up")
  p12pw=$(secret "LAN_P12_PASSWORD_$up")
  if [[ ! -f $out/users/$name.p12 ]]; then
    openssl req -newkey rsa:2048 -nodes -subj "/CN=$first $last/emailAddress=$email/O=oh-my-story LAN" \
      -keyout "$out/users/$name.key" -out "$out/users/$name.csr" 2>/dev/null
    openssl x509 -req -in "$out/users/$name.csr" -CA "$out/ca.crt" -CAkey "$out/ca.key" -CAcreateserial -days 825 -sha256 \
      -extfile <(printf '%s\n' \
        "subjectAltName=email:$email" \
        "basicConstraints=critical,CA:FALSE" \
        "keyUsage=critical,digitalSignature" \
        "extendedKeyUsage=clientAuth") \
      -out "$out/users/$name.crt" 2>/dev/null
    # AES-256 and SHA-256: what current Windows, macOS and browsers import.
    openssl pkcs12 -export -inkey "$out/users/$name.key" -in "$out/users/$name.crt" -certfile "$out/ca.crt" \
      -name "$first $last (OMS LAN)" -keypbe AES-256-CBC -certpbe AES-256-CBC -macalg sha256 \
      -passout "pass:$p12pw" -out "$out/users/$name.p12"
    rm -f "$out/users/$name.csr"
    echo "made a certificate for $name"
  fi
  printf '%-9s %-24s password %-22s certificate users/%s.p12 (file password %s)\n' "$name" "$email" "$pw" "$name" "$p12pw" >> "$out/users.txt"
done
chmod 600 "$out/ca.key" "$out"/users/*.key "$out/secrets.env" "$out/users.txt"
# Keycloak and Caddy read the server key as their own users inside their
# containers; out/ itself is private to you (700).
chmod 644 "$out/server.key"
echo "done: $(pwd)/$out"
