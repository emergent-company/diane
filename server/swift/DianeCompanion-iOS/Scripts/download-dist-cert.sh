#!/bin/bash
# download-dist-cert.sh — Download and import Apple Distribution certificate using App Store Connect API key
#
# Required env vars:
#   APP_STORE_CONNECT_KEY_ID     e.g. "JN6GMYYT5M"
#   APP_STORE_CONNECT_ISSUER_ID  e.g. "258506f5-..."
#   AUTH_KEY_PATH                Path to .p8 private key file
set -euo pipefail

if [ -z "${APP_STORE_CONNECT_KEY_ID:-}" ] || [ -z "${APP_STORE_CONNECT_ISSUER_ID:-}" ] || [ ! -f "${AUTH_KEY_PATH:-}" ]; then
    echo "⚠️  Missing API key credentials — skipping cert download"
    exit 0
fi

echo "==> Downloading Apple Distribution certificate..."

# Generate JWT using Ruby (built into macOS, has OpenSSL::PKey::EC)
JWT=$(ruby -e '
require "openssl"
require "base64"
key_id = ENV.fetch("APPLE_KEY_ID")
issuer = ENV.fetch("APPLE_ISSUER")
key = OpenSSL::PKey::EC.new(File.read(ENV.fetch("APPLE_KEY_PATH")))
now = Time.now.to_i
header = Base64.urlsafe_encode64("{\"alg\":\"ES256\",\"kid\":\"#{key_id}\",\"typ\":\"JWT\"}", padding: false)
payload = Base64.urlsafe_encode64("{\"iss\":\"#{issuer}\",\"iat\":#{now},\"exp\":#{now+1200},\"aud\":\"appstoreconnect-v1\"}", padding: false)
sign_input = "#{header}.#{payload}"
signature = Base64.urlsafe_encode64(key.sign("SHA256", sign_input), padding: false)
puts "#{sign_input}.#{signature}"
' 2>&1) || {
    echo "⚠️  Ruby JWT generation failed: $JWT"
    exit 0
}

# Fetch distribution certificates
RESP=$(curl -s --connect-timeout 10 \
    -H "Authorization: Bearer ${JWT}" \
    "https://api.appstoreconnect.apple.com/v1/certificates?filter%5BcertificateType%5D=DISTRIBUTION&sort=-id" 2>&1) || {
    echo "⚠️  API call failed: $RESP"
    exit 0
}

# Extract the first (most recent) distribution cert ID
CERT_ID=$(echo "$RESP" | ruby -e '
require "json"
data = JSON.parse(STDIN.read)
if data["data"] && data["data"].length > 0
    puts data["data"][0]["id"]
end
' 2>/dev/null) || true

if [ -z "$CERT_ID" ]; then
    echo "   No distribution certificates found in account"
    echo "   API response: $(echo "$RESP" | head -c 200)"
    exit 0
fi

echo "   Found cert ID: ${CERT_ID}"

# Download the certificate
CERT_PEM=$(curl -s --connect-timeout 10 \
    -H "Authorization: Bearer ${JWT}" \
    "https://api.appstoreconnect.apple.com/v1/certificates/${CERT_ID}/download" 2>&1) || {
    echo "⚠️  Cert download failed: $CERT_PEM"
    exit 0
}

if echo "$CERT_PEM" | grep -q "BEGIN CERTIFICATE"; then
    echo "$CERT_PEM" > /tmp/apple_dist_cert.pem
    security import /tmp/apple_dist_cert.pem -k ~/Library/Keychains/login.keychain-db 2>/dev/null || true
    rm -f /tmp/apple_dist_cert.pem
    sleep 1
    
    # Verify it was imported
    IDENTITY=$(security find-identity -v -p basic 2>/dev/null | grep "Apple Distribution" | head -1 || true)
    if [ -n "$IDENTITY" ]; then
        echo "✓ Imported: $(echo "$IDENTITY" | sed 's/.*"\(.*\)".*/\1/')"
    else
        echo "⚠️  Cert imported but identity not in keychain"
    fi
else
    echo "⚠️  Invalid cert response: $(echo "$CERT_PEM" | head -c 100)"
fi
