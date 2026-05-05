#!/bin/bash
# =================================================================
# tps-spam.sh — TPS benchmark vô hạn + giám sát hash lệch block
#
# Cách dùng:
#   ./tps-spam.sh              → Khởi động trong tmux (hoặc attach nếu đang chạy)
#   ./tps-spam.sh stop         → Dừng tmux session
#   ./tps-spam.sh log          → Xem TPS log từng round
#   ./tps-spam.sh errors       → Xem error log
#   ./tps-spam.sh _run         → (Nội bộ) Chạy vòng lặp TPS, gọi bởi tmux
# =================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TPS_DIR="$(cd "$SCRIPT_DIR/../test_tps/tps_blast_cc" && pwd)"
CHECKER_DIR="$(cd "$SCRIPT_DIR/../block/block_hash_checker" && pwd)"
SESSION="tps_loop"

# ── Cấu hình chạy TPS (override qua env var) ─────────────────────
COUNT="${COUNT:-20000}"
BATCH="${BATCH:-10}"
SLEEP_MS="${SLEEP_MS:-10}"
WAIT="${WAIT:-120}"
ROUNDS_PER_RUN="${ROUNDS_PER_RUN:-1}"
PARALLEL="${PARALLEL:-true}"
LOAD_BALANCE="${LOAD_BALANCE:-false}"

# ── Cấu hình block_hash_checker ───────────────────────────────────
CHECKER_INTERVAL="${CHECKER_INTERVAL:-5s}"
CHECKER_LAST="${CHECKER_LAST:-100}"
CHECKER_NODES="${CHECKER_NODES:-m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748}"

# ── Log files ─────────────────────────────────────────────────────
LOG_DIR="$SCRIPT_DIR/logs"
TPS_LOG="$LOG_DIR/tps_rounds.log"
ERR_LOG="$LOG_DIR/tps_errors.log"
FLAG_STOP="$LOG_DIR/stop_tps.flag"

# ── Hàm extract TPS từ output ────────────────────────────────────
extract_tps()    { grep -oP 'End-to-End TPS:\s+~?\K[0-9]+' <<< "$1" | tail -1; }
extract_blocks() { grep -oP 'Blocks \K[0-9]+ to [0-9]+' <<< "$1" | tail -1; }

# =================================================================
# ACTION: _run — Vòng lặp TPS (chạy bên trong tmux, không gọi trực tiếp)
# =================================================================
_run_loop() {
    mkdir -p "$LOG_DIR"

    SESSION_START=$(date '+%Y-%m-%d %H:%M:%S')
    {
        echo "================================================================"
        echo "  SESSION START: $SESSION_START"
        echo "  Config: count=$COUNT batch=$BATCH sleep=${SLEEP_MS}ms wait=${WAIT}s parallel=$PARALLEL"
        echo "================================================================"
    } | tee -a "$TPS_LOG"

    {
        echo "================================================================"
        echo "  SESSION START: $SESSION_START"
        echo "================================================================"
    } >> "$ERR_LOG"

    ROUND=0
    TRAP_FIRED=0
    trap 'echo ""; echo ""; echo "🛑 Nhận Ctrl+C, dừng sau khi kết thúc round hiện tại..."; TRAP_FIRED=1' INT

    cd "$TPS_DIR" || { echo "❌ Không tìm thấy thư mục TPS: $TPS_DIR"; exit 1; }

    while true; do
        if [ -f "$FLAG_STOP" ]; then
            echo ""
            echo "🛑 Đã nhận tín hiệu dừng (có thể do lệch hash). Dừng toàn bộ TPS test!" | tee -a "$TPS_LOG"
            break
        fi

        ROUND=$((ROUND + 1))
        TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

        echo ""
        echo "╔═══════════════════════════════════════════════════════════╗"
        echo "║  🔄 ROUND $ROUND — $TIMESTAMP"
        echo "╚═══════════════════════════════════════════════════════════╝"

        OUTPUT=$(go run main.go \
            --count "$COUNT" \
            --batch "$BATCH" \
            --sleep "$SLEEP_MS" \
            --wait "$WAIT" \
            --rounds "$ROUNDS_PER_RUN" \
            --parallel_native="$PARALLEL" \
            --load_balance="$LOAD_BALANCE" 2>&1)
        EXIT_CODE=$?

        echo "$OUTPUT"

        TPS=$(extract_tps "$OUTPUT");    [ -z "$TPS" ]         && TPS="N/A"
        BLOCK_RANGE=$(extract_blocks "$OUTPUT"); [ -z "$BLOCK_RANGE" ] && BLOCK_RANGE="N/A"

        if [ $EXIT_CODE -eq 0 ]; then
            echo "[$TIMESTAMP] Round $ROUND | TPS: ~${TPS} tx/s | Blocks: $BLOCK_RANGE | TXs: $COUNT" >> "$TPS_LOG"
            echo "  ✅ Round $ROUND OK → TPS: ~${TPS} tx/s"
        else
            {
                echo ""
                echo "[$TIMESTAMP] ❌ ROUND $ROUND FAILED (exit=$EXIT_CODE) | TPS: ~${TPS} tx/s | Blocks: $BLOCK_RANGE"
                echo "--- OUTPUT ---"
                echo "$OUTPUT"
                echo "--- END ---"
            } >> "$ERR_LOG"
            echo "[$TIMESTAMP] Round $ROUND | TPS: ~${TPS} tx/s | Blocks: $BLOCK_RANGE | TXs: $COUNT | ❌ FAILED (exit=$EXIT_CODE)" >> "$TPS_LOG"
            echo "  ❌ Round $ROUND FAILED! Xem: $ERR_LOG"
        fi

        # Summary min/max/avg TPS mỗi 10 round
        if (( ROUND % 10 == 0 )); then
            STATS=$(grep -oP 'TPS: ~\K[0-9]+' "$TPS_LOG" | awk '
                BEGIN { min=9999999; max=0; sum=0; n=0 }
                { n++; sum+=$1; if($1<min) min=$1; if($1>max) max=$1 }
                END { if(n>0) printf "n=%d | Min: ~%d tx/s | Max: ~%d tx/s | Avg: ~%d tx/s", n, min, max, sum/n }
            ')
            { echo ""; echo "  --- Stats sau $ROUND rounds: $STATS ---"; echo ""; } >> "$TPS_LOG"
        fi

        if [ "$TRAP_FIRED" -eq 1 ]; then
            echo ""
            echo "╔═══════════════════════════════════════════════════════════╗"
            echo "║  🏁 KẾT THÚC — Tổng $ROUND rounds đã chạy"
            echo "╚═══════════════════════════════════════════════════════════╝"
            echo ""
            echo "📊 TPS log: $TPS_LOG"
            echo "❌ Error log: $ERR_LOG"
            echo ""
            tail -"$ROUND" "$TPS_LOG" 2>/dev/null || tail -20 "$TPS_LOG"
            break
        fi
    done
}

# =================================================================
# MAIN — Xử lý subcommand
# =================================================================
ACTION="${1:-start}"

case "$ACTION" in
    _run)
        _run_loop
        ;;

    stop)
        if tmux has-session -t "$SESSION" 2>/dev/null; then
            tmux kill-session -t "$SESSION"
            echo "🛑 Đã dừng tmux session '$SESSION'."
        else
            echo "ℹ️  Không có session '$SESSION' nào đang chạy."
        fi
        ;;

    log)
        [ -f "$TPS_LOG" ] && tail -40 "$TPS_LOG" || echo "❌ Chưa có log: $TPS_LOG"
        ;;

    errors)
        [ -f "$ERR_LOG" ] && tail -60 "$ERR_LOG" || echo "✅ Không có lỗi nào."
        ;;

    start|*)
        # Xóa log cũ mỗi lần khởi động lại
        > "$ERR_LOG"
        > "$TPS_LOG"
        rm -f "$FLAG_STOP"

        if tmux has-session -t "$SESSION" 2>/dev/null; then
            echo "🔗 Session '$SESSION' đang chạy — attach vào window TPS..."
            tmux attach-session -t "${SESSION}:tps"
        else
            echo "🚀 Khởi động tmux session '$SESSION'..."

            # Window 1: TPS loop
            tmux new-session -d -s "$SESSION" -n "tps" -x 220 -y 50 \
                "COUNT=$COUNT BATCH=$BATCH SLEEP_MS=$SLEEP_MS WAIT=$WAIT PARALLEL=$PARALLEL LOAD_BALANCE=$LOAD_BALANCE bash $SCRIPT_DIR/tps-spam.sh _run; exec bash"

            # Window 2: block_hash_checker
            tmux new-window -t "$SESSION" -n "hash-check" \
                "cd $CHECKER_DIR && go run main.go --watch --interval $CHECKER_INTERVAL --check-last $CHECKER_LAST --nodes \"$CHECKER_NODES\" 2>> \"$ERR_LOG\" || { echo \"[\$(date '+%Y-%m-%d %H:%M:%S')] ❌ LỖI: Block Hash Checker đã dừng do phát hiện lệch hash (hoặc bị crash). Kiểm tra hash_mismatch_alert.log để biết chi tiết!\" >> \"$ERR_LOG\"; touch \"$FLAG_STOP\"; tmux send-keys -t \"${SESSION}:tps\" C-c; }; exec bash"

            # Quay lại window TPS (window đầu)
            tmux select-window -t "${SESSION}:tps"
            echo ""
            echo "✅ Session '$SESSION' đã khởi động!"
            echo ""
            echo "📌 Lệnh hữu ích:"
            echo "   Xem màn hình:  tmux attach -t $SESSION"
            echo "   Tách ra:       Ctrl+B rồi D"
            echo "   Dừng:          $0 stop"
            echo "   Xem TPS log:   $0 log"
            echo "   Xem lỗi:       $0 errors"
            echo ""
            read -r -p "🔗 Attach ngay? (y/N): " -t 5 ATTACH
            echo ""
            [[ "$ATTACH" =~ ^[Yy]$ ]] && tmux attach-session -t "$SESSION"
        fi
        ;;
esac
