# Nginx 反向代理配置指南

## 概述

NOFX 提供了基于 Nginx 的反向代理 Docker 镜像，支持 HTTPS (443) 和 HTTP (80)，可以统一前端访问入口。

## 架构

```
Internet
   │
   ├─ HTTP (80)  → 自动重定向到 HTTPS
   └─ HTTPS (443) → Nginx 反向代理
                      ├─ / → nofx-frontend:80 (前端)
                      └─ /api/ → nofx:8080 (后端 API)
```

## 快速开始

### 1. 生成 SSL 证书（开发环境）

使用提供的脚本生成自签名证书：

```bash
./scripts/generate-ssl-cert.sh
```

脚本会：
- 在 `./ssl` 目录生成 `cert.pem` 和 `key.pem`
- 支持自定义域名（默认：localhost）
- 自动设置正确的文件权限

### 2. 启动服务

使用包含 Nginx 代理的 docker-compose 配置：

```bash
docker compose -f docker-compose.nginx.yml up -d
```

### 3. 访问

- **HTTPS**: `https://localhost` 或 `https://your-domain.com`
- **HTTP**: `http://localhost` (自动重定向到 HTTPS)

## 生产环境 SSL 证书

### 使用 Let's Encrypt (推荐)

1. **安装 certbot**

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install certbot

# 或使用 Docker
docker run -it --rm \
  -v ./certbot/conf:/etc/letsencrypt \
  -v ./certbot/www:/var/www/certbot \
  certbot/certbot certonly --webroot \
  -w /var/www/certbot \
  -d your-domain.com
```

2. **挂载证书到容器**

```yaml
volumes:
  - ./certbot/conf/live/your-domain.com/fullchain.pem:/etc/nginx/ssl/cert.pem:ro
  - ./certbot/conf/live/your-domain.com/privkey.pem:/etc/nginx/ssl/key.pem:ro
```

3. **自动续期**

创建 cron 任务或使用 systemd timer：

```bash
# 添加到 crontab
0 0 * * * docker run --rm -v ./certbot/conf:/etc/letsencrypt -v ./certbot/www:/var/www/certbot certbot/certbot renew --quiet && docker compose -f docker-compose.nginx.yml restart nginx-proxy
```

### 使用其他 CA 证书

将您的证书文件放置到 `./ssl` 目录：

```bash
# 复制证书文件
cp your-certificate.crt ./ssl/cert.pem
cp your-private-key.key ./ssl/key.pem

# 设置权限
chmod 644 ./ssl/cert.pem
chmod 600 ./ssl/key.pem
```

## 环境变量配置

在 `.env` 文件中配置端口：

```env
# Nginx 端口配置
NGINX_HTTP_PORT=80
NGINX_HTTPS_PORT=443

# 后端端口（不直接暴露）
NOFX_BACKEND_PORT=8080

# 前端端口（不直接暴露）
NOFX_FRONTEND_PORT=3000
```

## 配置说明

### Nginx 配置特性

- ✅ **自动 HTTP → HTTPS 重定向**
- ✅ **现代 SSL/TLS 配置** (TLS 1.2/1.3)
- ✅ **安全响应头** (HSTS, X-Frame-Options, etc.)
- ✅ **Gzip 压缩**
- ✅ **长连接支持** (WebSocket)
- ✅ **扩展超时** (300s for API calls)
- ✅ **健康检查端点** (`/health`)

### 端口映射

| 服务 | 容器端口 | 主机端口 | 说明 |
|------|---------|---------|------|
| nginx-proxy | 80 | 80 | HTTP (重定向到 HTTPS) |
| nginx-proxy | 443 | 443 | HTTPS |
| nofx | 8080 | - | 仅内部访问 |
| nofx-frontend | 80 | - | 仅内部访问 |

## 故障排查

### 1. 证书错误

**问题**: 浏览器显示 "不安全连接"

**解决**:
- 检查证书文件是否存在: `ls -la ./ssl/`
- 验证证书格式: `openssl x509 -in ./ssl/cert.pem -text -noout`
- 对于自签名证书，需要在浏览器中添加例外

### 2. 502 Bad Gateway

**问题**: Nginx 无法连接到后端服务

**解决**:
```bash
# 检查服务是否运行
docker compose -f docker-compose.nginx.yml ps

# 检查网络连接
docker compose -f docker-compose.nginx.yml exec nginx-proxy ping nofx
docker compose -f docker-compose.nginx.yml exec nginx-proxy ping nofx-frontend

# 查看日志
docker compose -f docker-compose.nginx.yml logs nginx-proxy
```

### 3. 端口冲突

**问题**: 端口 80 或 443 已被占用

**解决**:
```bash
# 检查端口占用
sudo lsof -i :80
sudo lsof -i :443

# 修改 .env 中的端口
NGINX_HTTP_PORT=8080
NGINX_HTTPS_PORT=8443
```

## 高级配置

### 自定义 Nginx 配置

如果需要自定义配置，可以：

1. **修改 `nginx/nginx-ssl.conf`**
2. **重新构建镜像**:
   ```bash
   docker compose -f docker-compose.nginx.yml build nginx-proxy
   docker compose -f docker-compose.nginx.yml up -d
   ```

### 启用 HTTP/2

HTTP/2 已在配置中启用（`http2` 指令）。确保使用有效的 SSL 证书。

### 负载均衡（多实例）

如果需要负载均衡多个后端实例，可以修改 nginx 配置：

```nginx
upstream backend {
    least_conn;
    server nofx:8080;
    server nofx2:8080;
}

location /api/ {
    proxy_pass http://backend/api/;
    # ... 其他配置
}
```

## 安全建议

1. **使用强密码和密钥管理**
2. **定期更新 SSL 证书**
3. **启用防火墙规则**（仅开放 80/443）
4. **监控日志** (`docker compose logs nginx-proxy`)
5. **定期更新 Nginx 镜像**

## 相关文件

- `docker/Dockerfile.nginx-proxy` - Nginx 代理镜像构建文件
- `nginx/nginx-ssl.conf` - Nginx SSL 配置文件
- `docker-compose.nginx.yml` - 包含 Nginx 代理的完整配置
- `scripts/generate-ssl-cert.sh` - SSL 证书生成脚本
