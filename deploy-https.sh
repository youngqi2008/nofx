#!/bin/bash

# NOFX HTTPS部署脚本
# 快速部署HTTPS反向代理服务

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印标题
print_header() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  NOFX HTTPS反向代理部署工具${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
}

# 打印步骤
print_step() {
    echo -e "${GREEN}[步骤 $1]${NC} $2"
}

# 打印信息
print_info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

# 打印成功
print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

# 打印错误
print_error() {
    echo -e "${RED}✗${NC} $1"
}

# 检查命令是否存在
check_command() {
    if ! command -v $1 &> /dev/null; then
        print_error "$1 未安装，请先安装"
        exit 1
    fi
}

# 显示菜单
show_menu() {
    echo ""
    echo "请选择部署方式："
    echo ""
    echo "  1) 快速部署（使用自签名证书）"
    echo "  2) 生产部署（使用Let's Encrypt证书）"
    echo "  3) 使用已有证书"
    echo "  4) 退出"
    echo ""
    read -p "请输入选项 (1-4): " choice
}

# 快速部署（自签名证书）
quick_deploy() {
    print_header
    print_step "1/4" "检查依赖..."
    
    check_command "docker"
    check_command "docker compose"
    check_command "openssl"
    
    print_success "依赖检查完成"
    
    print_step "2/4" "生成自签名SSL证书..."
    
    # 创建SSL目录
    mkdir -p nginx/ssl
    
    # 获取服务器IP
    echo ""
    print_info "请输入服务器公网IP地址（按回车使用localhost）："
    read -r SERVER_IP
    
    if [ -z "$SERVER_IP" ]; then
        SERVER_IP="localhost"
    fi
    
    # 生成证书（兼容旧版本OpenSSL）
    # 检查OpenSSL版本是否支持-addext
    OPENSSL_VERSION=$(openssl version | awk '{print $2}' | cut -d'.' -f1-2)
    
    if openssl req -help 2>&1 | grep -q '\-addext'; then
        # OpenSSL 1.1.1+ 支持 -addext
        openssl req -x509 -nodes -days 365 \
            -newkey rsa:2048 \
            -keyout nginx/ssl/server.key \
            -out nginx/ssl/server.crt \
            -subj "/C=CN/ST=State/L=City/O=NOFX/OU=Trading/CN=$SERVER_IP" \
            -addext "subjectAltName=DNS:localhost,DNS:$SERVER_IP,IP:127.0.0.1,IP:$SERVER_IP"
    else
        # 旧版本OpenSSL，使用配置文件方式
        cat > /tmp/openssl-san.cnf << EOF
[req]
default_bits = 2048
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = v3_req

[dn]
C=CN
ST=State
L=City
O=NOFX
OU=Trading
CN=$SERVER_IP

[v3_req]
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = $SERVER_IP
IP.1 = 127.0.0.1
IP.2 = $SERVER_IP
EOF
        openssl req -x509 -nodes -days 365 \
            -newkey rsa:2048 \
            -keyout nginx/ssl/server.key \
            -out nginx/ssl/server.crt \
            -config /tmp/openssl-san.cnf \
            -extensions v3_req
        rm -f /tmp/openssl-san.cnf
    fi
    
    if [ $? -ne 0 ]; then
        print_error "SSL证书生成失败"
        echo "请检查OpenSSL是否正确安装"
        openssl version
        exit 1
    fi
    
    chmod 600 nginx/ssl/server.key
    chmod 644 nginx/ssl/server.crt
    
    print_success "SSL证书生成成功"
    
    print_step "3/4" "配置环境变量..."
    
    # 检查.env文件
    if [ ! -f ".env" ]; then
        print_info "创建.env文件..."
        cat > .env << EOF
# NOFX环境变量配置
NOFX_TIMEZONE=Asia/Shanghai
DATA_ENCRYPTION_KEY=$(openssl rand -base64 32)
JWT_SECRET=$(openssl rand -base64 64)
EOF
        print_success ".env文件创建成功"
    else
        print_info ".env文件已存在，跳过"
    fi
    
    # 检查config.json
    if [ ! -f "config.json" ]; then
        if [ -f "config.json.example" ]; then
            print_info "复制配置文件..."
            cp config.json.example config.json
            print_success "config.json创建成功"
        else
            print_error "config.json.example不存在"
            exit 1
        fi
    else
        print_info "config.json已存在，跳过"
    fi
    
    print_step "4/4" "启动服务..."
    
    docker compose -f docker-compose.yml -f docker-compose.https.yml up -d
    
    echo ""
    print_success "部署完成！"
    echo ""
    echo -e "${YELLOW}访问地址：${NC}"
    echo -e "  HTTPS: ${GREEN}https://$SERVER_IP${NC}"
    echo -e "  HTTP:  ${GREEN}http://$SERVER_IP${NC} (自动重定向到HTTPS)"
    echo ""
    echo -e "${YELLOW}注意：${NC}"
    echo "  • 使用自签名证书，浏览器会显示安全警告"
    echo "  • 需要在浏览器中手动信任该证书"
    echo "  • 不推荐用于生产环境"
    echo ""
    echo -e "${YELLOW}查看日志：${NC}"
    echo "  docker compose -f docker-compose.yml -f docker-compose.https.yml logs -f"
    echo ""
}

# 生产部署（Let's Encrypt）
production_deploy() {
    print_header
    print_step "1/5" "检查依赖..."
    
    check_command "docker"
    check_command "docker compose"
    check_command "certbot"
    
    print_success "依赖检查完成"
    
    print_step "2/5" "配置域名信息..."
    
    echo ""
    print_info "请输入您的域名（例如：nofx.example.com）："
    read -r DOMAIN
    
    if [ -z "$DOMAIN" ]; then
        print_error "域名不能为空"
        exit 1
    fi
    
    print_info "请输入您的邮箱（用于接收证书过期通知）："
    read -r EMAIL
    
    if [ -z "$EMAIL" ]; then
        print_error "邮箱不能为空"
        exit 1
    fi
    
    echo ""
    print_info "域名: $DOMAIN"
    print_info "邮箱: $EMAIL"
    echo ""
    
    print_info "重要提示："
    echo "  • 确保域名已正确解析到当前服务器IP"
    echo "  • 确保80端口和443端口对外开放"
    echo ""
    
    read -p "确认信息无误? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "已取消"
        exit 0
    fi
    
    print_step "3/5" "获取Let's Encrypt证书..."
    
    # 创建SSL目录
    mkdir -p nginx/ssl
    
    # 停止可能运行的nginx容器
    docker compose -f docker-compose.yml -f docker-compose.https.yml stop nginx-proxy 2>/dev/null || true
    
    # 获取证书
    sudo certbot certonly \
        --standalone \
        --preferred-challenges http \
        --email "$EMAIL" \
        --agree-tos \
        --no-eff-email \
        -d "$DOMAIN"
    
    # 复制证书
    sudo cp "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" nginx/ssl/server.crt
    sudo cp "/etc/letsencrypt/live/$DOMAIN/privkey.pem" nginx/ssl/server.key
    
    # 设置权限
    sudo chown $USER:$USER nginx/ssl/server.crt nginx/ssl/server.key
    chmod 644 nginx/ssl/server.crt
    chmod 600 nginx/ssl/server.key
    
    print_success "证书获取成功"
    
    print_step "4/5" "配置环境变量..."
    
    # 检查.env文件
    if [ ! -f ".env" ]; then
        print_info "创建.env文件..."
        cat > .env << EOF
# NOFX环境变量配置
NOFX_TIMEZONE=Asia/Shanghai
DATA_ENCRYPTION_KEY=$(openssl rand -base64 32)
JWT_SECRET=$(openssl rand -base64 64)
EOF
        print_success ".env文件创建成功"
    else
        print_info ".env文件已存在，跳过"
    fi
    
    # 检查config.json
    if [ ! -f "config.json" ]; then
        if [ -f "config.json.example" ]; then
            print_info "复制配置文件..."
            cp config.json.example config.json
            print_success "config.json创建成功"
        else
            print_error "config.json.example不存在"
            exit 1
        fi
    else
        print_info "config.json已存在，跳过"
    fi
    
    print_step "5/5" "启动服务..."
    
    docker compose -f docker-compose.yml -f docker-compose.https.yml up -d
    
    echo ""
    print_success "部署完成！"
    echo ""
    echo -e "${YELLOW}访问地址：${NC}"
    echo -e "  HTTPS: ${GREEN}https://$DOMAIN${NC}"
    echo ""
    echo -e "${YELLOW}证书自动续期：${NC}"
    echo "  Let's Encrypt证书有效期为90天，certbot会自动续期"
    echo "  测试续期命令: sudo certbot renew --dry-run"
    echo ""
    echo -e "${YELLOW}查看日志：${NC}"
    echo "  docker compose -f docker-compose.yml -f docker-compose.https.yml logs -f"
    echo ""
}

# 使用已有证书
custom_cert_deploy() {
    print_header
    print_step "1/3" "配置SSL证书..."
    
    mkdir -p nginx/ssl
    
    echo ""
    print_info "请提供您的SSL证书文件路径："
    read -p "证书文件(.crt/.pem): " CERT_PATH
    read -p "私钥文件(.key): " KEY_PATH
    
    if [ ! -f "$CERT_PATH" ]; then
        print_error "证书文件不存在: $CERT_PATH"
        exit 1
    fi
    
    if [ ! -f "$KEY_PATH" ]; then
        print_error "私钥文件不存在: $KEY_PATH"
        exit 1
    fi
    
    # 复制证书
    cp "$CERT_PATH" nginx/ssl/server.crt
    cp "$KEY_PATH" nginx/ssl/server.key
    
    # 设置权限
    chmod 644 nginx/ssl/server.crt
    chmod 600 nginx/ssl/server.key
    
    print_success "证书配置完成"
    
    print_step "2/3" "配置环境变量..."
    
    # 检查.env文件
    if [ ! -f ".env" ]; then
        print_info "创建.env文件..."
        cat > .env << EOF
# NOFX环境变量配置
NOFX_TIMEZONE=Asia/Shanghai
DATA_ENCRYPTION_KEY=$(openssl rand -base64 32)
JWT_SECRET=$(openssl rand -base64 64)
EOF
        print_success ".env文件创建成功"
    else
        print_info ".env文件已存在，跳过"
    fi
    
    # 检查config.json
    if [ ! -f "config.json" ]; then
        if [ -f "config.json.example" ]; then
            print_info "复制配置文件..."
            cp config.json.example config.json
            print_success "config.json创建成功"
        else
            print_error "config.json.example不存在"
            exit 1
        fi
    else
        print_info "config.json已存在，跳过"
    fi
    
    print_step "3/3" "启动服务..."
    
    docker compose -f docker-compose.yml -f docker-compose.https.yml up -d
    
    echo ""
    print_success "部署完成！"
    echo ""
    echo -e "${YELLOW}查看日志：${NC}"
    echo "  docker compose -f docker-compose.yml -f docker-compose.https.yml logs -f"
    echo ""
}

# 主程序
main() {
    print_header
    
    show_menu
    
    case $choice in
        1)
            quick_deploy
            ;;
        2)
            production_deploy
            ;;
        3)
            custom_cert_deploy
            ;;
        4)
            print_info "退出"
            exit 0
            ;;
        *)
            print_error "无效选项"
            exit 1
            ;;
    esac
}

# 运行主程序
main


