#!/bin/bash

# 生成自签名SSL证书脚本
# 用于NOFX项目HTTPS反向代理

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# SSL证书目录
SSL_DIR="nginx/ssl"
CERT_FILE="$SSL_DIR/server.crt"
KEY_FILE="$SSL_DIR/server.key"

echo -e "${GREEN}=== NOFX SSL证书生成工具 ===${NC}"
echo ""

# 创建SSL目录
if [ ! -d "$SSL_DIR" ]; then
    echo -e "${YELLOW}创建SSL证书目录: $SSL_DIR${NC}"
    mkdir -p "$SSL_DIR"
fi

# 检查是否已存在证书
if [ -f "$CERT_FILE" ] && [ -f "$KEY_FILE" ]; then
    echo -e "${YELLOW}警告: SSL证书已存在${NC}"
    read -p "是否覆盖现有证书? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${GREEN}保留现有证书，退出${NC}"
        exit 0
    fi
fi

# 获取服务器IP地址（可选）
echo -e "${YELLOW}请输入服务器的公网IP地址（按回车跳过，使用localhost）:${NC}"
read -r SERVER_IP

if [ -z "$SERVER_IP" ]; then
    SERVER_IP="localhost"
fi

echo ""
echo -e "${GREEN}开始生成SSL证书...${NC}"
echo -e "服务器地址: ${YELLOW}$SERVER_IP${NC}"
echo ""

# 生成私钥和证书
openssl req -x509 -nodes -days 365 \
    -newkey rsa:2048 \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -subj "/C=CN/ST=State/L=City/O=NOFX/OU=Trading/CN=$SERVER_IP" \
    -addext "subjectAltName=DNS:localhost,DNS:$SERVER_IP,IP:127.0.0.1,IP:$SERVER_IP" \
    2>/dev/null

# 设置文件权限
chmod 600 "$KEY_FILE"
chmod 644 "$CERT_FILE"

echo ""
echo -e "${GREEN}✓ SSL证书生成成功！${NC}"
echo ""
echo -e "证书文件: ${YELLOW}$CERT_FILE${NC}"
echo -e "私钥文件: ${YELLOW}$KEY_FILE${NC}"
echo -e "有效期: ${YELLOW}365天${NC}"
echo ""
echo -e "${YELLOW}注意: 这是自签名证书，浏览器会显示安全警告。${NC}"
echo -e "${YELLOW}生产环境建议使用Let's Encrypt等CA签发的证书。${NC}"
echo ""
echo -e "${GREEN}证书信息:${NC}"
openssl x509 -in "$CERT_FILE" -noout -subject -dates

echo ""
echo -e "${GREEN}=== 完成 ===${NC}"


