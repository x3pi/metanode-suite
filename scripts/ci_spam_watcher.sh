#!/bin/bash
# ============================================================================
# ci_spam_watcher.sh
# Git CI Daemon: lắng nghe code mới trên branch → ansible reset-all →
# update-ip → bật monitors ngầm (node health + block hash) → chạy TPS test
# → thông báo kết quả qua Telegram
#
# Cách dùng:
#   ./ci_spam_watcher.sh              # Chạy foreground (để xem log)
#   ./ci_spam_watcher.sh --daemon     # Chạy ngầm nền (nohup)
#   ./ci_spam_watcher.sh --once       # Chỉ chạy 1 lần (không vòng lặp)
#   ./ci_spam_watcher.sh --branch main  # Theo dõi nhánh khác
#   ./ci_spam_watcher.sh --stop       # Dừng daemon đang chạy
# ============================================================================

set -u

# ─── Thư mục gốc ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
METANODE_DIR="$(cd "$SUITE_DIR/../metanode" && pwd)"
ANSIBLE_DIR="$METANODE_DIR/deploy/ansible"
TPS_BLAST_DIR="$SUITE_DIR/test_tps/tps_blast_cc"
BLOCK_CHECKER_DIR="$SUITE_DIR/block/block_hash_checker"
UPDATE_IP_SCRIPT="$SCRIPT_DIR/update-ip/update-ip.sh"
LOG_FILE="$SCRIPT_DIR/ci_spam_watcher.log"
PID_FILE="$SCRIPT_DIR/ci_spam_watcher.pid"
LAST_COMMIT_FILE="$SCRIPT_DIR/.ci_last_deployed_commit"

# ─── Cấu hình mặc định ──────────────────────────────────────────────────────
REMOTE="origin"
BRANCH="dev"
CHECK_INTERVAL=10            # Giây giữa mỗi lần poll git

# ─── Tham số TPS test ────────────────────────────────────────────────────────
TPS_COUNT=20000
TPS_ROUNDS=20000
TPS_BATCH=10
TPS_AMOUNT=1
TPS_CONFIG="config-multi.json"
TPS_LOAD_BALANCE=false

# ─── Đọc .env để lấy Telegram token ────────────────────────────────────────
for env_path in "$SCRIPT_DIR/.env" "$SCRIPT_DIR/../.env" "$METANODE_DIR/deploy/ansible/.env"; do
    if [ -f "$env_path" ]; then
        while IFS='=' read -r key val || [ -n "$key" ]; do
            key=$(echo "$key" | xargs)
            val=$(echo "$val" | xargs | sed "s/^['\"]//;s/['\"]$//")
            [[ -z "$key" || "$key" =~ ^# ]] && continue
            [[ -z "${!key:-}" ]] && export "$key"="$val" 2>/dev/null || true
        done < "$env_path"
    fi
done

TELEGRAM_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
TELEGRAM_CHAT_ID="${TELEGRAM_CHAT_ID:--1003867050625}"

# ─── PIDs các tiến trình ngầm ───────────────────────────────────────────────
HEALTH_PID=""
CHECKER_PID=""
TPS_PID=""

# ─── Parse arguments ────────────────────────────────────────────────────────
DAEMON_MODE=false
ONCE_MODE=false
DO_STOP=false

for arg in "$@"; do
    case "$arg" in
        --daemon|-d)    DAEMON_MODE=true ;;
        --once)         ONCE_MODE=true ;;
        --stop)         DO_STOP=true ;;
        --branch=*)     BRANCH="${arg#--branch=}" ;;
        --branch)       shift; BRANCH="${1:-dev}" ;;
        --interval=*)   CHECK_INTERVAL="${arg#--interval=}" ;;
    esac
done

# ─── Lệnh stop ──────────────────────────────────────────────────────────────
if [ "$DO_STOP" = true ]; then
    if [ -f "$PID_FILE" ]; then
        OLD_PID=$(cat "$PID_FILE")
        echo "🛑 Dừng ci_spam_watcher (PID: $OLD_PID)..."
        kill "$OLD_PID" 2>/dev/null || true
        rm -f "$PID_FILE"
        echo "✅ Đã dừng."
    else
        echo "⚠️  Không tìm thấy PID file. Tiến trình có thể đã dừng."
        pkill -f "ci_spam_watcher.sh" || true
    fi
    exit 0
fi

# ─── Chế độ daemon ──────────────────────────────────────────────────────────
if [ "$DAEMON_MODE" = true ]; then
    echo "🚀 Khởi động CI Git Watcher ở chế độ daemon..."
    nohup "$0" "${@/--daemon/}" "${@/-d/}" > "$LOG_FILE" 2>&1 &
    DAEMON_PID=$!
    echo "$DAEMON_PID" > "$PID_FILE"
    echo "✅ Watcher đang chạy nền (PID: $DAEMON_PID)"
    echo "📜 Xem log: tail -f $LOG_FILE"
    echo "🛑 Dừng: $0 --stop"
    exit 0
fi

# ─── Lưu PID hiện tại (foreground) ─────────────────────────────────────────
echo "$$" > "$PID_FILE"

# ═══════════════════════════════════════════════════════════════════════════
# HÀM TIỆN ÍCH
# ═══════════════════════════════════════════════════════════════════════════

ts() { date '+%Y-%m-%d %H:%M:%S'; }

log() { echo "[$(ts)] $*"; }

send_tele() {
    local msg="$1"
    local token="${TELEGRAM_BOT_TOKEN:-}"
    local chat="${TELEGRAM_CHAT_ID:--1003867050625}"
    [ -z "$token" ] && return
    curl -s -X POST "https://api.telegram.org/bot${token}/sendMessage" \
        -d chat_id="${chat}" \
        -d parse_mode="HTML" \
        --data-urlencode text="${msg}" > /dev/null 2>&1 || true
}

send_tele_notify() {
    local title="$1"
    local body="$2"
    local emoji="${3:-ℹ️}"
    local hostname_info
    hostname_info=$(hostname -I | awk '{print $1}' 2>/dev/null || echo "unknown")
    send_tele "${emoji} <b>${title}</b>
<b>Server:</b> <code>${hostname_info}</code>
<b>Branch:</b> <code>${BRANCH}</code>
<b>Time:</b> $(ts)

${body}"
}

# ─── Dừng tất cả tiến trình ngầm cũ ────────────────────────────────────────
stop_background_processes() {
    log "🧹 Dừng các tiến trình monitor ngầm cũ..."
    
    # Xóa cờ dừng cũ
    rm -f /tmp/MTN_CHAIN_ERROR_STOP

    # Kill TPS test đang chạy nếu có
    if [ -n "$TPS_PID" ] && kill -0 "$TPS_PID" 2>/dev/null; then
        log "   → Kill TPS test (PID: $TPS_PID)"
        kill "$TPS_PID" 2>/dev/null || true
        TPS_PID=""
    fi

    # Kill health monitor đang chạy
    if [ -n "$HEALTH_PID" ] && kill -0 "$HEALTH_PID" 2>/dev/null; then
        log "   → Kill health monitor (PID: $HEALTH_PID)"
        kill "$HEALTH_PID" 2>/dev/null || true
        HEALTH_PID=""
    fi

    # Kill block hash checker đang chạy
    if [ -n "$CHECKER_PID" ] && kill -0 "$CHECKER_PID" 2>/dev/null; then
        log "   → Kill block hash checker (PID: $CHECKER_PID)"
        kill "$CHECKER_PID" 2>/dev/null || true
        CHECKER_PID=""
    fi

    # Kill theo pattern để chắc chắn không còn sót
    pkill -f "tps_blast_cc.*go run" || true
    pkill -f "block_hash_checker.*--watch" || true
    pkill -f "start_monitors.sh health" || true
    pkill -f "ci_spam_watcher_health" || true

    sleep 1
    log "✅ Đã dừng hết tiến trình cũ."
}

# ─── Bước 1: Ansible reset-all & deploy ─────────────────────────────────────
run_ansible_deploy() {
    local commit_info="$1"
    log "🚀 [DEPLOY] Bắt đầu ansible reset-all cho commit: ${commit_info}"
    send_tele_notify "🔄 Auto Deploy Triggered" "Phát hiện commit mới, đang reset-all và deploy lại cluster...
<b>Commit:</b> <code>${commit_info}</code>" "🔄"

    cd "$ANSIBLE_DIR"
    # ansible_deploy.sh --start --clean = reset-all + clean + deploy
    if ! ./ansible_deploy.sh --start --clean >> "$LOG_FILE" 2>&1; then
        log "❌ [DEPLOY] ansible_deploy.sh thất bại!"
        send_tele_notify "❌ Deploy FAILED" "ansible_deploy.sh --start --clean thất bại!
<b>Commit:</b> <code>${commit_info}</code>

Kiểm tra log: <code>tail -f $LOG_FILE</code>" "❌"
        return 1
    fi

    log "✅ [DEPLOY] Ansible deploy hoàn tất!"
    return 0
}

# ─── Bước 2: Update IP configs ───────────────────────────────────────────────
run_update_ip() {
    log "🗺️  [UPDATE-IP] Cập nhật cấu hình IP cho các tool..."
    if [ -f "$UPDATE_IP_SCRIPT" ]; then
        if ! bash "$UPDATE_IP_SCRIPT" >> "$LOG_FILE" 2>&1; then
            log "⚠️  [UPDATE-IP] update-ip.sh có lỗi (không ngừng pipeline)"
        else
            log "✅ [UPDATE-IP] Cập nhật IP xong."
        fi
    else
        log "⚠️  [UPDATE-IP] Không tìm thấy $UPDATE_IP_SCRIPT, bỏ qua."
    fi
}

# ─── Bước 3: Bật monitors ngầm ───────────────────────────────────────────────
start_monitors() {
    log "🔭 [MONITORS] Khởi động các monitor ngầm qua start_monitors.sh..."
    local START_MON_SCRIPT="$ANSIBLE_DIR/monitors/start_monitors.sh"
    
    if [ -f "$START_MON_SCRIPT" ]; then
        # Gọi script start_monitors.sh
        bash "$START_MON_SCRIPT" >> "$LOG_FILE" 2>&1
        log "✅ [MONITORS] Đã gọi start_monitors.sh thành công."
    else
        log "⚠️  [MONITORS] Không tìm thấy $START_MON_SCRIPT!"
    fi
}

# ─── Bước 4: Chạy bài test TPS ────────────────────────────────────────────────
run_tps_test() {
    local commit_info="$1"
    log "🔥 [TPS] Bắt đầu chạy bài test TPS..."
    send_tele_notify "🔥 TPS Test Started" "Bắt đầu chạy bài test TPS sau deploy thành công.
<b>Commit:</b> <code>${commit_info}</code>
<b>Config:</b> ${TPS_CONFIG} | count=${TPS_COUNT} | rounds=${TPS_ROUNDS}" "🔥"

    local tps_log="$TPS_BLAST_DIR/tps_ci_$(date +%Y%m%d_%H%M%S).log"
    local start_time
    start_time=$(date +%s)

    (
        cd "$TPS_BLAST_DIR"
        go run main.go \
            --count "$TPS_COUNT" \
            --rounds "$TPS_ROUNDS" \
            --load_balance="$TPS_LOAD_BALANCE" \
            --batch="$TPS_BATCH" \
            --amount "$TPS_AMOUNT" \
            --config="$TPS_CONFIG"
        echo "TPS_EXIT_CODE=$?"
    ) > "$tps_log" 2>&1 &
    TPS_PID=$!
    log "   → TPS test đang chạy ngầm (PID: $TPS_PID), log: $tps_log"

    # Chờ TPS test hoàn tất
    local tps_exit=0
    wait "$TPS_PID" 2>/dev/null || tps_exit=$?
    TPS_PID=""

    local end_time
    end_time=$(date +%s)
    local elapsed=$(( end_time - start_time ))
    local elapsed_fmt
    elapsed_fmt=$(printf '%02d:%02d:%02d' $((elapsed/3600)) $((elapsed%3600/60)) $((elapsed%60)))

    # Đọc kết quả từ log
    local last_lines
    last_lines=$(tail -n 30 "$tps_log" 2>/dev/null || echo "Không có log")

    # Kiểm tra cờ lỗi hệ thống
    if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        local stop_reason
        stop_reason=$(cat /tmp/MTN_CHAIN_ERROR_STOP)
        log "🚨 [TPS] Phát hiện lỗi chain trong quá trình test!"
        send_tele_notify "🚨 TPS Test STOPPED - Chain Error" "Bài test TPS bị dừng do phát hiện lỗi chuỗi!
<b>Commit:</b> <code>${commit_info}</code>
<b>Thời gian chạy:</b> ${elapsed_fmt}
<b>Lý do:</b> <code>${stop_reason}</code>

<b>Log cuối:</b>
<pre>${last_lines}</pre>" "🚨"
        return 1
    fi

    if [ "$tps_exit" -ne 0 ]; then
        log "❌ [TPS] Bài test TPS thất bại (exit code: $tps_exit)"
        send_tele_notify "❌ TPS Test FAILED" "Bài test TPS thất bại!
<b>Commit:</b> <code>${commit_info}</code>
<b>Exit code:</b> ${tps_exit}
<b>Thời gian:</b> ${elapsed_fmt}

<b>Log cuối (30 dòng):</b>
<pre>${last_lines}</pre>" "❌"
        return 1
    fi

    # Tìm TPS summary từ log
    local tps_summary
    tps_summary=$(grep -E "(TPS|tps|Injection|confirmed|✅)" "$tps_log" 2>/dev/null | tail -n 10 || echo "Xem log: $tps_log")

    log "✅ [TPS] Bài test TPS hoàn tất sau ${elapsed_fmt}!"
    send_tele_notify "✅ TPS Test PASSED" "Bài test TPS chạy thành công! 🎉
<b>Commit:</b> <code>${commit_info}</code>
<b>Thời gian:</b> ${elapsed_fmt}
<b>Count:</b> ${TPS_COUNT} | <b>Rounds:</b> ${TPS_ROUNDS}

<b>Kết quả:</b>
<pre>${tps_summary}</pre>" "✅"
    return 0
}

# ─── CLEANUP khi script bị kill ─────────────────────────────────────────────
cleanup() {
    log "🛑 Nhận tín hiệu dừng, đang cleanup..."
    stop_background_processes
    rm -f "$PID_FILE"
    log "👋 ci_spam_watcher đã dừng."
    exit 0
}
trap cleanup INT TERM EXIT

# ═══════════════════════════════════════════════════════════════════════════
# HÀM PIPELINE CHÍNH: trigger khi phát hiện commit mới
# ═══════════════════════════════════════════════════════════════════════════
run_ci_pipeline() {
    local commit_hash="$1"
    local commit_msg="$2"
    local commit_author="$3"
    local commit_info="${commit_hash::8} by ${commit_author}: \"${commit_msg}\""

    log "═══════════════════════════════════════════════════════"
    log "🔔 PIPELINE STARTED: $commit_info"
    log "═══════════════════════════════════════════════════════"

    # Dừng tất cả tiến trình cũ trước
    stop_background_processes

    # Bước 1: Deploy
    if ! run_ansible_deploy "$commit_info"; then
        log "❌ Pipeline dừng tại bước Deploy."
        return 1
    fi

    # Bước 2: Update IP
    run_update_ip

    # Bước 3: Bật monitors ngầm
    start_monitors

    # Bước 4: Chạy TPS test
    if ! run_tps_test "$commit_info"; then
        log "❌ Pipeline kết thúc với lỗi ở bước TPS Test."
        return 1
    fi

    log "🎉 CI PIPELINE HOÀN TẤT THÀNH CÔNG!"
    log "═══════════════════════════════════════════════════════"
}

# ═══════════════════════════════════════════════════════════════════════════
# MAIN LOOP
# ═══════════════════════════════════════════════════════════════════════════
log "════════════════════════════════════════════════════════"
log "👀 CI Spam Watcher khởi động"
log "📍 Metanode: $METANODE_DIR"
log "📍 Suite: $SUITE_DIR"
log "📍 Branch: $REMOTE/$BRANCH"
log "⏰ Check interval: ${CHECK_INTERVAL}s"
log "════════════════════════════════════════════════════════"

cd "$METANODE_DIR"

# Đảm bảo đang theo dõi đúng nhánh
git checkout "$BRANCH" 2>/dev/null || true

# Lấy commit hiện tại làm baseline
if git fetch "$REMOTE" "$BRANCH" > /dev/null 2>&1; then
    CURRENT_REMOTE=$(git rev-parse "${REMOTE}/${BRANCH}")
else
    CURRENT_REMOTE=$(git rev-parse HEAD)
fi

if [ -f "$LAST_COMMIT_FILE" ]; then
    LAST_DEPLOYED=$(cat "$LAST_COMMIT_FILE" | xargs)
    log "📌 Commit đã deploy cuối: ${LAST_DEPLOYED::8}"
else
    # Lần đầu chạy: lưu commit hiện tại và trigger pipeline ngay
    echo "$CURRENT_REMOTE" > "$LAST_COMMIT_FILE"
    LAST_DEPLOYED=""
    log "📌 Lần đầu khởi động — sẽ trigger pipeline ngay với commit hiện tại"
fi

# Nếu là lần đầu chạy hoặc chế độ --once: trigger ngay
if [ -z "$LAST_DEPLOYED" ] || [ "$ONCE_MODE" = true ]; then
    git pull "$REMOTE" "$BRANCH" > /dev/null 2>&1 || true
    COMMIT_HASH=$(git rev-parse HEAD)
    COMMIT_MSG=$(git log -1 --pretty=%B | head -n 1)
    COMMIT_AUTHOR=$(git log -1 --pretty=%an)
    run_ci_pipeline "$COMMIT_HASH" "$COMMIT_MSG" "$COMMIT_AUTHOR" || true
    echo "$COMMIT_HASH" > "$LAST_COMMIT_FILE"

    if [ "$ONCE_MODE" = true ]; then
        log "✅ Chế độ --once: đã xong. Thoát."
        exit 0
    fi
fi

log "👀 Bắt đầu vòng lặp poll git mỗi ${CHECK_INTERVAL}s..."
send_tele_notify "👀 CI Spam Watcher Started" "Đang theo dõi nhánh <code>${BRANCH}</code> để phát hiện commit mới.
Check interval: <code>${CHECK_INTERVAL}s</code>" "👀"

while true; do
    sleep "$CHECK_INTERVAL"

    # Poll git
    if ! git fetch "$REMOTE" "$BRANCH" > /dev/null 2>&1; then
        log "⚠️  [$(ts)] Không fetch được từ git. Thử lại sau..."
        continue
    fi

    REMOTE_HASH=$(git rev-parse "${REMOTE}/${BRANCH}")
    LAST_DEPLOYED=$(cat "$LAST_COMMIT_FILE" 2>/dev/null | xargs || echo "")

    if [ "$REMOTE_HASH" = "$LAST_DEPLOYED" ]; then
        # Không có gì mới, kiểm tra các tiến trình ngầm còn sống không
        continue
    fi

    # Có commit mới!
    log ""
    log "🔔 [$(ts)] Phát hiện commit mới trên ${REMOTE}/${BRANCH}!"
    log "   Cũ: ${LAST_DEPLOYED::8}"
    log "   Mới: ${REMOTE_HASH::8}"

    # Pull về
    cd "$METANODE_DIR"
    git pull "$REMOTE" "$BRANCH" > /dev/null 2>&1 || true

    NEW_LOCAL_HASH=$(git rev-parse HEAD)
    COMMIT_MSG=$(git log -1 --pretty=%B | head -n 1)
    COMMIT_AUTHOR=$(git log -1 --pretty=%an)

    # Chạy pipeline
    run_ci_pipeline "$NEW_LOCAL_HASH" "$COMMIT_MSG" "$COMMIT_AUTHOR" || true

    # Lưu commit đã deploy
    echo "$NEW_LOCAL_HASH" > "$LAST_COMMIT_FILE"

    log "👀 Tiếp tục theo dõi..."
done
