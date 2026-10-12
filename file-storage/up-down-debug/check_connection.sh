#!/bin/bash

# ==============================================================================
# Script kiểm tra kết nối mạng (TCP, UDP, QUIC) cho MetaNode Suite & Rust Storage
# ==============================================================================

# Màu sắc
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

ENV_FILE="${1:-.env.1}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${CYAN}================================================================${NC}"
echo -e "${CYAN}  🔍 KIỂM TRA KẾT NỐI HỆ THỐNG METANODE & RUST STORAGE${NC}"
echo -e "${CYAN}================================================================${NC}"

# Đọc cấu hình từ file .env nếu có
if [ -f "$ENV_FILE" ]; then
    echo -e "${BLUE}📄 Đang đọc cấu hình từ: $ENV_FILE${NC}"
    # Lấy host từ các biến môi trường
    RUST_1=$(grep -E '^RUST_SERVER_1=' "$ENV_FILE" | cut -d '=' -f2- | tr -d ' "\r')
    RUST_2=$(grep -E '^RUST_SERVER_2=' "$ENV_FILE" | cut -d '=' -f2- | tr -d ' "\r')
    RPC=$(grep -E '^RPC_URL=' "$ENV_FILE" | cut -d '=' -f2- | tr -d ' "\r')
    TCP=$(grep -E '^TCP_URL=' "$ENV_FILE" | cut -d '=' -f2- | tr -d ' "\r')
fi

# Fallback mặc định nếu chưa cấu hình
RUST_1="${RUST_1:-hkb0av57jsh.sn.mynetname.net:7081}"
RUST_2="${RUST_2:-hkb0av57jsh.sn.mynetname.net:7082}"

# Tách Host và Port
HOST_1=$(echo "$RUST_1" | cut -d: -f1)
PORT_1=$(echo "$RUST_1" | cut -d: -f2)

HOST_2=$(echo "$RUST_2" | cut -d: -f1)
PORT_2=$(echo "$RUST_2" | cut -d: -f2)

# Lấy host kiểm tra chung
TARGET_HOST="$HOST_1"

echo -e "🎯 Target Host: ${YELLOW}$TARGET_HOST${NC}"
echo ""

# 1. Kiểm tra DNS & Ping
echo -e "${BLUE}[1/4] Kiểm tra DNS & Ping tới $TARGET_HOST...${NC}"
RESOLVED_IP=$(getent ahosts "$TARGET_HOST" | awk '{print $1}' | head -n 1)
if [ -n "$RESOLVED_IP" ]; then
    echo -e "  ✅ DNS Lookup: ${GREEN}$TARGET_HOST -> $RESOLVED_IP${NC}"
else
    echo -e "  ❌ DNS Lookup: ${RED}Không thể phân giải tên miền $TARGET_HOST${NC}"
    exit 1
fi

if ping -c 2 -W 2 "$TARGET_HOST" > /dev/null 2>&1; then
    PING_TIME=$(ping -c 2 -W 2 "$TARGET_HOST" | tail -1 | awk -F '/' '{print $5}')
    echo -e "  ✅ Ping: ${GREEN}Host phản hồi tốt (avg: ${PING_TIME}ms)${NC}"
else
    echo -e "  ⚠️  Ping: ${YELLOW}Host không phản hồi Ping (có thể router chặn ICMP)${NC}"
fi

echo ""

# Hàm test TCP port
test_tcp() {
    local host=$1
    local port=$2
    local desc=$3
    printf "  %-30s : " "$desc ($port)"
    if nc -zv -w 3 "$host" "$port" > /dev/null 2>&1; then
        echo -e "${GREEN}✅ OPEN (Thành công)${NC}"
        return 0
    else
        echo -e "${RED}❌ CLOSED / TIMEOUT (Bị chặn/chưa NAT)${NC}"
        return 1
    fi
}

# 2. Kiểm tra các Port TCP
echo -e "${BLUE}[2/4] Kiểm tra các cổng TCP (Handshake 3 bước)...${NC}"
test_tcp "$TARGET_HOST" "8747" "Blockchain RPC"
test_tcp "$TARGET_HOST" "4200" "MetaNode TCP Ingress"
test_tcp "$HOST_1" "$PORT_1" "Rust Storage Server 1"
test_tcp "$HOST_2" "$PORT_2" "Rust Storage Server 2"

echo ""

# 3. Kiểm tra UDP bằng nping
echo -e "${BLUE}[3/4] Kiểm tra phản hồi UDP (nping)...${NC}"
if command -v nping &> /dev/null; then
    check_nping() {
        local host=$1
        local port=$2
        local out
        out=$(nping --udp -p "$port" -c 2 "$host" 2>&1)
        local lost
        lost=$(echo "$out" | grep -o "Lost: [0-9]* ([0-9.]*%)" | head -1)
        if echo "$out" | grep -q "Lost: 0 (0.00%)"; then
            echo -e "  UDP Port $port: ${GREEN}✅ Phản hồi tốt ($lost)${NC}"
        else
            echo -e "  UDP Port $port: ${RED}❌ Không phản hồi ($lost - Router chưa forward UDP)${NC}"
        fi
    }
    check_nping "$HOST_1" "$PORT_1"
    check_nping "$HOST_2" "$PORT_2"
else
    echo -e "  ${YELLOW}nping chưa được cài đặt, bỏ qua bước này.${NC}"
fi

echo ""

# 4. Kiểm tra bắt tay QUIC thực tế (quic-go)
echo -e "${BLUE}[4/4] Kiểm tra QUIC Handshake thực tế (ALPN: file-storage-v1)...${NC}"
if [ -f "./check_quic.go" ]; then
    check_quic_server() {
        local addr=$1
        echo -e "  👉 Đang kết nối QUIC tới ${YELLOW}$addr${NC}..."
        local quic_out
        quic_out=$(go run check_quic.go -addr "$addr" -timeout 3 2>&1)
        if echo "$quic_out" | grep -q "KẾT NỐI QUIC THÀNH CÔNG"; then
            local dur
            dur=$(echo "$quic_out" | grep "KẾT NỐI QUIC THÀNH CÔNG" | grep -o "(mất [^)]*)")
            echo -e "     ${GREEN}✅ THÀNH CÔNG! $dur${NC}"
        else
            echo -e "     ${RED}❌ THẤT BẠI: Timeout (Chưa mở UDP trên Router hoặc Firewall chặn)${NC}"
        fi
    }
    check_quic_server "$RUST_1"
    check_quic_server "$RUST_2"
else
    echo -e "  ${YELLOW}Không tìm thấy check_quic.go${NC}"
fi

echo ""
echo -e "${CYAN}================================================================${NC}"
echo -e "${CYAN}  📋 TỔNG KẾT${NC}"
echo -e "${CYAN}================================================================${NC}"
echo -e "  - Nếu TCP 8747 & 4200 ${GREEN}OPEN${NC}: Blockchain node hoạt động bình thường."
echo -e "  - Nếu 7081 & 7082 ${RED}TIMEOUT${NC}: Cần thêm rule NAT trên Router:"
echo -e "      Chain: ${YELLOW}dstnat${NC} | Protocol: ${YELLOW}udp${NC} | Dst.Port: ${YELLOW}7081, 7082${NC}"
echo -e "      To-Addresses: ${YELLOW}IP máy server Rust trong LAN${NC}"
echo -e "${CYAN}================================================================${NC}"
