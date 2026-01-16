#!/bin/bash
# generate-ssl-cert.sh - Generate self-signed SSL certificate for development

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SSL_DIR="$PROJECT_ROOT/ssl"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🔐 SSL Certificate Generator${NC}"
echo ""

# Create SSL directory if it doesn't exist
mkdir -p "$SSL_DIR"

# Check if certificate already exists
if [ -f "$SSL_DIR/cert.pem" ] && [ -f "$SSL_DIR/key.pem" ]; then
    echo -e "${YELLOW}⚠️  SSL certificate already exists${NC}"
    read -p "Do you want to regenerate? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Keeping existing certificate."
        exit 0
    fi
    rm -f "$SSL_DIR/cert.pem" "$SSL_DIR/key.pem"
fi

# Get domain name (default: localhost)
read -p "Enter domain name (default: localhost): " DOMAIN
DOMAIN=${DOMAIN:-localhost}

echo ""
echo -e "${GREEN}Generating self-signed certificate for: ${DOMAIN}${NC}"

# Generate private key
openssl genrsa -out "$SSL_DIR/key.pem" 2048

# Generate certificate signing request and certificate
openssl req -new -x509 -key "$SSL_DIR/key.pem" -out "$SSL_DIR/cert.pem" -days 365 \
    -subj "/C=US/ST=State/L=City/O=NOFX/CN=${DOMAIN}" \
    -addext "subjectAltName=DNS:${DOMAIN},DNS:*.${DOMAIN},IP:127.0.0.1,IP:::1"

# Set proper permissions
chmod 600 "$SSL_DIR/key.pem"
chmod 644 "$SSL_DIR/cert.pem"

echo ""
echo -e "${GREEN}✅ SSL certificate generated successfully!${NC}"
echo ""
echo "Certificate location:"
echo "  - Certificate: $SSL_DIR/cert.pem"
echo "  - Private Key: $SSL_DIR/key.pem"
echo ""
echo -e "${YELLOW}⚠️  Note: This is a self-signed certificate for development only.${NC}"
echo "For production, use certificates from Let's Encrypt or a trusted CA."
echo ""
echo "To use with docker-compose, ensure the ssl directory is mounted:"
echo "  volumes:"
echo "    - ./ssl:/etc/nginx/ssl:ro"
