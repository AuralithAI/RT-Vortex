#!/bin/bash
#
# Generate self-signed TLS certificates for AI PR Reviewer
# 
# Usage: ./generate-certs.sh [output-dir]
#
# Default output: ../config/certificates/
#

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT_DIR="${1:-$SCRIPT_DIR/../config/certificates}"
DAYS=365
KEY_SIZE=2048

# Certificate subjects
CA_SUBJ="/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=AIPR-CA"
SERVER_SUBJ="/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=localhost"
CLIENT_SUBJ="/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=aipr-client"

echo "========================================"
echo " AI PR Reviewer - Certificate Generator"
echo "========================================"
echo ""
echo "Output directory: $OUTPUT_DIR"
echo "Validity: $DAYS days"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"
cd "$OUTPUT_DIR"

# Generate CA
echo "[1/5] Generating CA certificate..."
openssl genrsa -out ca.key $KEY_SIZE 2>/dev/null
openssl req -new -x509 -days $DAYS -key ca.key -out ca.crt -subj "$CA_SUBJ" 2>/dev/null
echo "      Created: ca.key, ca.crt"

# Generate Server Certificate
echo "[2/5] Generating server certificate..."
openssl genrsa -out server.key $KEY_SIZE 2>/dev/null
openssl req -new -key server.key -out server.csr -subj "$SERVER_SUBJ" 2>/dev/null

# Create SAN config for localhost
cat > server_san.cnf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = localhost

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = *.localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

openssl x509 -req -days $DAYS -in server.csr -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out server.crt -extfile server_san.cnf -extensions v3_req 2>/dev/null
rm -f server.csr server_san.cnf ca.srl
echo "      Created: server.key, server.crt"

# Generate Client Certificate (for mTLS)
echo "[3/5] Generating client certificate..."
openssl genrsa -out client.key $KEY_SIZE 2>/dev/null
openssl req -new -key client.key -out client.csr -subj "$CLIENT_SUBJ" 2>/dev/null

cat > client_san.cnf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = aipr-client

[v3_req]
keyUsage = digitalSignature
extendedKeyUsage = clientAuth
EOF

openssl x509 -req -days $DAYS -in client.csr -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out client.crt -extfile client_san.cnf -extensions v3_req 2>/dev/null
rm -f client.csr client_san.cnf ca.srl
echo "      Created: client.key, client.crt"

# Set permissions
echo "[4/5] Setting permissions..."
chmod 644 *.crt
chmod 600 *.key
echo "      Certificates: 644, Keys: 600"

# Verify
echo "[5/5] Verifying certificates..."
openssl verify -CAfile ca.crt server.crt >/dev/null 2>&1 && echo "      Server cert: OK"
openssl verify -CAfile ca.crt client.crt >/dev/null 2>&1 && echo "      Client cert: OK"

echo ""
echo "========================================"
echo " Certificates Generated Successfully!"
echo "========================================"
echo ""
echo "Files created in: $OUTPUT_DIR"
echo ""
ls -la "$OUTPUT_DIR"/*.crt "$OUTPUT_DIR"/*.key 2>/dev/null | awk '{print "  " $9 " (" $5 " bytes)"}'
echo ""
echo "WARNING: These are self-signed certificates for DEVELOPMENT ONLY."
echo "         For production, use certificates from a trusted CA."
echo ""
