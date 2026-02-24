#!/bin/bash
# setup-postgres.sh - Setup and start PostgreSQL with Docker Compose
#
# This script:
#   1. Creates the Docker network if it doesn't exist
#   2. Generates .env file with PostgreSQL configuration
#   3. Starts PostgreSQL service
#   4. Provides connection instructions

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${GREEN}🐘 PostgreSQL Docker Setup${NC}"
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed. Please install Docker first.${NC}"
    exit 1
fi

if ! command -v docker compose &> /dev/null && ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed. Please install Docker Compose first.${NC}"
    exit 1
fi

# Use docker compose (v2) or docker-compose (v1)
DOCKER_COMPOSE_CMD="docker compose"
if ! command -v docker compose &> /dev/null; then
    DOCKER_COMPOSE_CMD="docker-compose"
fi

# Create Docker network if it doesn't exist
echo -e "${BLUE}📡 Checking Docker network...${NC}"
if ! docker network inspect nofx-network &> /dev/null; then
    echo -e "${YELLOW}Creating Docker network: nofx-network${NC}"
    docker network create nofx-network
    echo -e "${GREEN}✓ Network created${NC}"
else
    echo -e "${GREEN}✓ Network already exists${NC}"
fi
echo ""

# Check if .env file exists
ENV_FILE="$PROJECT_ROOT/.env"
if [ ! -f "$ENV_FILE" ]; then
    echo -e "${YELLOW}⚠️  .env file not found. Creating from template...${NC}"
    touch "$ENV_FILE"
fi

# Generate PostgreSQL password if not set
if ! grep -q "POSTGRES_PASSWORD=" "$ENV_FILE" 2>/dev/null || [ -z "$(grep "POSTGRES_PASSWORD=" "$ENV_FILE" | cut -d'=' -f2)" ]; then
    POSTGRES_PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-25)
    echo "" >> "$ENV_FILE"
    echo "# PostgreSQL Configuration" >> "$ENV_FILE"
    echo "POSTGRES_USER=postgres" >> "$ENV_FILE"
    echo "POSTGRES_PASSWORD=$POSTGRES_PASSWORD" >> "$ENV_FILE"
    echo "POSTGRES_DB=nofx" >> "$ENV_FILE"
    echo "POSTGRES_PORT=5432" >> "$ENV_FILE"
    echo "" >> "$ENV_FILE"
    echo "# Database Configuration for NOFX" >> "$ENV_FILE"
    echo "DB_TYPE=postgres" >> "$ENV_FILE"
    echo "DB_HOST=postgres" >> "$ENV_FILE"
    echo "DB_PORT=5432" >> "$ENV_FILE"
    echo "DB_USER=postgres" >> "$ENV_FILE"
    echo "DB_PASSWORD=$POSTGRES_PASSWORD" >> "$ENV_FILE"
    echo "DB_NAME=nofx" >> "$ENV_FILE"
    echo "DB_SSLMODE=disable" >> "$ENV_FILE"
    echo "" >> "$ENV_FILE"
    echo -e "${GREEN}✓ Generated PostgreSQL configuration in .env${NC}"
    echo -e "${YELLOW}⚠️  PostgreSQL password: ${POSTGRES_PASSWORD}${NC}"
    echo -e "${YELLOW}⚠️  Please save this password securely!${NC}"
else
    # Load existing password
    POSTGRES_PASSWORD=$(grep "POSTGRES_PASSWORD=" "$ENV_FILE" | cut -d'=' -f2 | head -n1)
    echo -e "${GREEN}✓ Using existing PostgreSQL configuration${NC}"
fi
echo ""

# Create required directories
echo -e "${BLUE}📁 Creating required directories...${NC}"
mkdir -p "$PROJECT_ROOT/docker/postgres/init"
mkdir -p "$PROJECT_ROOT/docker/postgres/log"
echo -e "${GREEN}✓ Directories created${NC}"
echo ""

# Start PostgreSQL
echo -e "${BLUE}🚀 Starting PostgreSQL...${NC}"
cd "$PROJECT_ROOT"
$DOCKER_COMPOSE_CMD -f docker-compose.postgres.yml up -d postgres

# Wait for PostgreSQL to be ready
echo -e "${BLUE}⏳ Waiting for PostgreSQL to be ready...${NC}"
MAX_RETRIES=30
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec nofx-postgres pg_isready -U postgres -d nofx &> /dev/null; then
        echo -e "${GREEN}✓ PostgreSQL is ready!${NC}"
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo -n "."
    sleep 1
done
echo ""

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo -e "${RED}❌ PostgreSQL failed to start within ${MAX_RETRIES} seconds${NC}"
    echo -e "${YELLOW}Check logs with: docker logs nofx-postgres${NC}"
    exit 1
fi

# Display connection information
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✅ PostgreSQL is running!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}Connection Information:${NC}"
echo -e "  Host:     ${GREEN}localhost${NC} (from host) or ${GREEN}postgres${NC} (from Docker)"
echo -e "  Port:     ${GREEN}5432${NC}"
echo -e "  Database: ${GREEN}nofx${NC}"
echo -e "  User:     ${GREEN}postgres${NC}"
echo -e "  Password: ${GREEN}${POSTGRES_PASSWORD}${NC}"
echo ""
echo -e "${BLUE}Connection String:${NC}"
echo -e "  ${YELLOW}postgresql://postgres:${POSTGRES_PASSWORD}@localhost:5432/nofx?sslmode=disable${NC}"
echo ""
echo -e "${BLUE}Useful Commands:${NC}"
echo -e "  ${YELLOW}docker logs nofx-postgres${NC}              - View PostgreSQL logs"
echo -e "  ${YELLOW}docker exec -it nofx-postgres psql -U postgres -d nofx${NC}  - Connect to database"
echo -e "  ${YELLOW}docker compose -f docker-compose.postgres.yml stop${NC}     - Stop PostgreSQL"
echo -e "  ${YELLOW}docker compose -f docker-compose.postgres.yml down${NC}     - Stop and remove containers"
echo -e "  ${YELLOW}docker compose -f docker-compose.postgres.yml down -v${NC}  - Remove containers and volumes (⚠️ deletes data)"
echo ""
echo -e "${BLUE}Optional: Start pgAdmin (Database Management UI)${NC}"
echo -e "  ${YELLOW}docker compose -f docker-compose.postgres.yml --profile tools up -d pgadmin${NC}"
echo -e "  Then access at: ${GREEN}http://localhost:5050${NC}"
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
