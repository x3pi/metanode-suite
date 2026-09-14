#!/usr/bin/env bash
# ==============================================================================
# 🛡️  METANODE FAULT-TOLERANCE SUITE
# Formalizes/extends restart-recovery/run_restart_test.sh's proven rolling-
# restart chaos test into a set of explicit BFT failure-class scenarios, each
# with its own pass/fail criterion, informed by the failure classes actually
# root-caused on this project (DIGEST-GATE deadlock, STARTUP-SYNC fork,
# quorum-loss boundary, CatchingUp quorum-discovery phase-flip).
#
# SCENARIOS (select with --scenarios, default: all):
#   rolling  — delegates to the existing, proven run_restart_test.sh (one
#              loop): rolling single-node restart + full-cluster restart,
#              zero-fork verified before/after. No new risk; free coverage.
#   quorum   — the BFT quorum-loss boundary, f = floor((n-1)/3):
#                (a) stop f nodes simultaneously → cluster MUST stay live
#                    (remaining n-f nodes keep confirming tx) — restore, verify
#                    zero-fork reconvergence.
#                (b) stop f+1 nodes simultaneously → cluster MUST safely HALT
#                    (block height on the remaining nodes must NOT advance —
#                    the halt-not-guess invariant) rather than fork or guess —
#                    restore full cluster, verify it resumes and reconverges
#                    zero-fork.
#              This is the scenario class never before deliberately exercised
#              end-to-end on this project — see reference_metanode_distributed_
#              cluster.md / bft_fault_tolerance_node_count.md for the formula.
#   catchup  — stop one node, let the rest advance a large number of blocks
#              (forcing a real CatchingUp path with non-trivial lag rather than
#              a same-tick restart), restore it, verify zero-fork convergence.
#              Exercises the STARTUP-SYNC / CatchingUp code paths this project
#              has twice found real forks in.
#
# NOT included (scoped out deliberately, not an oversight): a true hard-crash
# / SIGKILL / power-loss scenario. Doing that safely needs a new privileged
# ansible action (today's stop_services role only does graceful SIGTERM with a
# hardened 90s wait+escalate sequence — see its own header comment for a past
# subtle bug in that exact logic) rather than a test script reaching for sudo
# itself. consensus/metanode/scripts/test_ansible_partition.sh already does a
# SIGSTOP/SIGCONT freeze for a *network-partition* flavor of this, but embeds
# a hardcoded plaintext sudo password — do not copy that pattern into new
# scripts; flagged separately for cleanup, not fixed here.
#
# Exit code: 0 only if every selected scenario PASSed. A results summary
# (human-readable + JSON) prints at the end either way.
# ==============================================================================

set -uo pipefail  # NOT -e: one scenario failing must not abort the others —
                   # each scenario is wrapped by run_scenario, which records
                   # PASS/FAIL and continues, matching this suite's whole point
                   # (surface every failing class in one run, not just the
                   # first one alphabetically).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_PATH="${SCRIPT_DIR}/../config.json"
RESTART_DIR="${SCRIPT_DIR}/../restart-recovery"

DEFAULT_METANODE_DIR="$(cd "${SCRIPT_DIR}/../../../../../metanode" 2>/dev/null && pwd || true)"
METANODE_DIR="${METANODE_DIR:-"$DEFAULT_METANODE_DIR"}"
ANSIBLE_DIR="${METANODE_DIR}/deploy/ansible"

if [ ! -f "${ANSIBLE_DIR}/ansible_deploy.sh" ]; then
    echo "❌ Không tìm thấy script ansible_deploy.sh tại: ${ANSIBLE_DIR}"
    exit 1
fi

SCENARIOS="all"
TX_COUNT=15
CATCHUP_BLOCKS=30
RESULTS_JSON="${SCRIPT_DIR}/last_run_results.json"

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --scenarios) SCENARIOS="$2"; shift ;;
        --scenarios=*) SCENARIOS="${1#*=}" ;;
        --count) TX_COUNT="$2"; shift ;;
        --count=*) TX_COUNT="${1#*=}" ;;
        --catchup-blocks) CATCHUP_BLOCKS="$2"; shift ;;
        --catchup-blocks=*) CATCHUP_BLOCKS="${1#*=}" ;;
        -h|--help)
            echo "Cách dùng: $0 [OPTIONS]"
            echo "  --scenarios <all|rolling,quorum,catchup>   Chọn scenario cần chạy (mặc định: all)"
            echo "  --count <15>              Số lượng giao dịch kiểm chứng mỗi chặng"
            echo "  --catchup-blocks <30>     Số block tối thiểu cụm phải tạo thêm trong lúc node bị dừng ở scenario 'catchup'"
            exit 0
            ;;
        *) echo "Tham số không hợp lệ: $1"; exit 1 ;;
    esac
    shift
done

if [ "$SCENARIOS" == "all" ]; then
    SCENARIO_LIST=(rolling quorum catchup)
else
    IFS=',' read -r -a SCENARIO_LIST <<< "$SCENARIOS"
fi

echo "=========================================================="
echo "🛡️  METANODE FAULT-TOLERANCE SUITE"
echo "📂 Thư mục: ${SCRIPT_DIR}"
echo "⚙️  Ansible: ${ANSIBLE_DIR}/ansible_deploy.sh"
echo "🎯 Scenarios: [ ${SCENARIO_LIST[*]} ]"
echo "=========================================================="

cd "${SCRIPT_DIR}"
trap 'rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true' EXIT

source "${SCRIPT_DIR}/../lib/cluster_helpers.sh"
source "${SCRIPT_DIR}/../lib/consensus_readiness.sh"

echo "🔍 Đang tự động quét phát hiện các node đang hoạt động..."
DETECTED=$(detect_online_nodes || true)
if [ -z "$DETECTED" ]; then
    echo "❌ Không phát hiện được node nào online từ ${CONFIG_PATH}! Dừng bài test."
    exit 1
fi
read -r -a ACTIVE_NODES <<< "$DETECTED"
N_NODES=${#ACTIVE_NODES[@]}
F_TOLERANCE=$(( (N_NODES - 1) / 3 ))

echo "📋 Danh sách node tham gia: [ ${ACTIVE_NODES[*]} ] (Tổng: ${N_NODES} nodes, f = ${F_TOLERANCE})"
if [ "$N_NODES" != "4" ]; then
    echo "⚠️  Cảnh báo: cụm auto-detect không phải 4 node [0 1 2 3] — nếu đây không phải cụm local 232,"
    echo "   HÃY DỪNG NGAY. Chạy 'metanode-suite/scripts/update-ip/update-ip.sh --chain public' để trỏ lại"
    echo "   về cụm local trước khi tiếp tục (xem reference_metanode_distributed_cluster.md)."
fi

declare -A RESULT_STATUS
declare -A RESULT_DETAIL
SUITE_START=$(date +%s)

# run_scenario NAME FUNC — runs FUNC, records PASS/FAIL, never aborts the suite.
run_scenario() {
    local name="$1"
    local func="$2"
    echo -e "\n\n=========================================================="
    echo "▶️  [SCENARIO: ${name}] BẮT ĐẦU"
    echo "=========================================================="
    local t0=$(date +%s)
    if "$func"; then
        local t1=$(date +%s)
        RESULT_STATUS["$name"]="PASS"
        RESULT_DETAIL["$name"]="completed in $((t1 - t0))s"
        echo "✅ [SCENARIO: ${name}] PASS ($((t1 - t0))s)"
    else
        local t1=$(date +%s)
        RESULT_STATUS["$name"]="FAIL"
        RESULT_DETAIL["$name"]="failed after $((t1 - t0))s — see log above for the exact assertion"
        echo "❌ [SCENARIO: ${name}] FAIL ($((t1 - t0))s)"
    fi
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true
}

# ------------------------------------------------------------------------------
# SCENARIO: rolling — delegate to the existing, already-proven script.
# ------------------------------------------------------------------------------
scenario_rolling() {
    (cd "${RESTART_DIR}" && ./run_restart_test.sh --count "${TX_COUNT}" --loop 1)
}

# ------------------------------------------------------------------------------
# SCENARIO: quorum — BFT quorum-loss boundary (f vs f+1 nodes down).
# ------------------------------------------------------------------------------
scenario_quorum() {
    if [ "$F_TOLERANCE" -lt 1 ]; then
        echo "ℹ️  f=${F_TOLERANCE} (cụm quá nhỏ để chịu lỗi) — bỏ qua scenario quorum."
        return 0
    fi

    verify_equal_height_and_zero_fork "QUORUM-BOUNDARY khởi đầu" 240 || return 1

    # Pick the LAST f nodes in ACTIVE_NODES as the "down" set for part (a).
    local f_nodes=("${ACTIVE_NODES[@]: -${F_TOLERANCE}}")
    local f_nodes_csv=$(IFS=,; echo "${f_nodes[*]}")
    local f_nodes_ssv="${f_nodes[*]}"

    echo -e "\n----------------------------------------------------------"
    echo "🔴 [QUORUM 1/2] Dừng đồng thời f=${F_TOLERANCE} node [ ${f_nodes_ssv} ] — cụm PHẢI vẫn sống (còn $((N_NODES - F_TOLERANCE)) >= 2f+1)"
    echo "----------------------------------------------------------"
    echo "${f_nodes_ssv}" > /tmp/monitors_ignore_nodes 2>/dev/null || true
    for n in "${f_nodes[@]}"; do
        "${ANSIBLE_DIR}/ansible_deploy.sh" --stop --only-node "${n}"
    done
    wait_nodes_offline "${f_nodes_ssv}" 30 || { echo "❌ Node không dừng được, huỷ scenario."; return 1; }

    echo "⚡ LIVENESS CHECK: gửi ${TX_COUNT} tx qua các node còn sống (loại trừ [${f_nodes_csv}])..."
    if ! (cd "${RESTART_DIR}" && go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true -stopped-node="${f_nodes_csv}"); then
        echo "❌ LIVENESS THẤT BẠI: cụm không xử lý được tx dù chỉ mất f=${F_TOLERANCE} node — vi phạm BFT fault-tolerance kỳ vọng!"
        # best-effort restore before bailing
        for n in "${f_nodes[@]}"; do "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${n}"; done
        return 1
    fi
    echo "✅ Cụm vẫn xử lý tx bình thường khi mất f=${F_TOLERANCE} node (đúng như kỳ vọng BFT)."

    echo "🟢 Bật lại [ ${f_nodes_ssv} ]..."
    for n in "${f_nodes[@]}"; do
        "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${n}"
        wait_node_online "${n}" || return 1
        wait_node_consensus_ready "${n}"
    done
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true
    verify_equal_height_and_zero_fork "QUORUM-BOUNDARY sau khi khôi phục f node" 240 || return 1

    # Part (b): f+1 nodes down at once — must be enough to break quorum. Guard
    # against a cluster too small for this half to make sense (need at least 1
    # node left alive to observe the halt-not-advance behavior on).
    local f_plus_1=$((F_TOLERANCE + 1))
    if [ "$f_plus_1" -ge "$N_NODES" ]; then
        echo "ℹ️  f+1=${f_plus_1} >= N=${N_NODES} (không còn node nào để quan sát) — bỏ qua phần an toàn (b)."
        return 0
    fi

    local fp1_nodes=("${ACTIVE_NODES[@]: -${f_plus_1}}")
    local fp1_ssv="${fp1_nodes[*]}"
    local remaining_nodes=("${ACTIVE_NODES[@]:0:$((N_NODES - f_plus_1))}")

    echo -e "\n----------------------------------------------------------"
    echo "🔴 [QUORUM 2/2] Dừng đồng thời f+1=${f_plus_1} node [ ${fp1_ssv} ] — cụm PHẢI HALT AN TOÀN (không tiến thêm block, không fork)"
    echo "----------------------------------------------------------"
    echo "${fp1_ssv}" > /tmp/monitors_ignore_nodes 2>/dev/null || true
    for n in "${fp1_nodes[@]}"; do
        "${ANSIBLE_DIR}/ansible_deploy.sh" --stop --only-node "${n}"
    done
    wait_nodes_offline "${fp1_ssv}" 30 || { echo "❌ Node không dừng được, huỷ scenario."; return 1; }

    echo "⏳ Chờ 10s ổn định rồi lấy baseline chiều cao các node còn sống [ ${remaining_nodes[*]} ]..."
    sleep 10
    local heights_before=""
    for n in "${remaining_nodes[@]}"; do
        heights_before="${heights_before}${n}=$(get_node_height "$n") "
    done
    echo "   Baseline: ${heights_before}"

    echo "⏳ [SAFETY WINDOW] Theo dõi 60s: chiều cao KHÔNG được tiến lên (halt-not-guess invariant)..."
    local safety_ok=true
    local samples=0
    while [ "$samples" -lt 6 ]; do
        sleep 10
        samples=$((samples + 1))
        local heights_now=""
        for n in "${remaining_nodes[@]}"; do
            local h_before h_now
            h_before=$(echo "$heights_before" | tr ' ' '\n' | grep "^${n}=" | cut -d= -f2)
            h_now=$(get_node_height "$n")
            heights_now="${heights_now}${n}=${h_now} "
            if [ -n "$h_before" ] && [ -n "$h_now" ] && [ "$h_now" -gt "$h_before" ]; then
                safety_ok=false
            fi
        done
        echo "   [sample ${samples}/6] ${heights_now}"
    done

    for n in "${fp1_nodes[@]}"; do
        "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${n}"
    done
    wait_all_nodes_online 90 || true
    wait_all_nodes_consensus_ready "${ACTIVE_NODES[*]}" 90 || true
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

    if [ "$safety_ok" != "true" ]; then
        echo "🚨 VI PHẠM AN TOÀN: chiều cao TIẾN LÊN dù mất quorum (f+1=${f_plus_1} node down)! Đây có thể là dấu hiệu fork/guess — CẦN điều tra ngay."
        return 1
    fi
    echo "✅ Cụm HALT AN TOÀN đúng như kỳ vọng khi mất quorum (không tiến block, không đoán mò)."

    echo "🔍 Khôi phục toàn bộ cụm, xác nhận resume + zero-fork..."
    verify_equal_height_and_zero_fork "QUORUM-BOUNDARY sau khi khôi phục toàn bộ cụm" 240 || return 1
    (cd "${RESTART_DIR}" && go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true) || return 1
}

# ------------------------------------------------------------------------------
# SCENARIO: catchup — one node down for a real lag window, then rejoins.
# ------------------------------------------------------------------------------
scenario_catchup() {
    verify_equal_height_and_zero_fork "CATCHUP khởi đầu" 240 || return 1

    local target="${ACTIVE_NODES[0]}"
    local start_height
    start_height=$(get_node_height "$target")
    echo "🔴 Dừng Node ${target} (đang ở block #${start_height:-?})..."
    echo "${target}" > /tmp/monitors_ignore_nodes 2>/dev/null || true
    "${ANSIBLE_DIR}/ansible_deploy.sh" --stop --only-node "${target}"
    wait_node_offline "${target}" 30 || return 1

    echo "⚡ Bơm tx liên tục qua các node còn lại cho tới khi cụm tiến thêm >= ${CATCHUP_BLOCKS} block (tạo lag thật sự)..."
    local other_node=""
    for n in "${ACTIVE_NODES[@]}"; do
        if [ "$n" != "$target" ]; then other_node="$n"; break; fi
    done
    local waited=0
    local max_wait=300
    while [ "$waited" -lt "$max_wait" ]; do
        (cd "${RESTART_DIR}" && go run main.go -count "${TX_COUNT}" -check-fork=false -require-all-alive=false -stopped-node="${target}") >/dev/null 2>&1 || true
        local now_height
        now_height=$(get_node_height "$other_node")
        local gained=$(( ${now_height:-0} - ${start_height:-0} ))
        echo "   ... lag hiện tại: +${gained} block (mục tiêu >= ${CATCHUP_BLOCKS})"
        if [ "$gained" -ge "$CATCHUP_BLOCKS" ]; then
            break
        fi
        sleep 3
        waited=$((waited + 3))
    done

    echo "🟢 Bật lại Node ${target} sau khi cụm đã tiến xa (CatchingUp path thật sự)..."
    "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${target}"
    wait_node_online "${target}" 90 || return 1
    wait_node_consensus_ready "${target}" 180
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

    echo "🔍 Xác nhận Node ${target} bắt kịp và toàn cụm zero-fork..."
    verify_equal_height_and_zero_fork "CATCHUP sau khi Node ${target} bắt kịp" 300
}

for name in "${SCENARIO_LIST[@]}"; do
    case "$name" in
        rolling) run_scenario "rolling" scenario_rolling ;;
        quorum) run_scenario "quorum" scenario_quorum ;;
        catchup) run_scenario "catchup" scenario_catchup ;;
        *) echo "⚠️  Bỏ qua scenario không hợp lệ: ${name}" ;;
    esac
done

SUITE_END=$(date +%s)

echo -e "\n\n=========================================================="
echo "🏁 TỔNG KẾT FAULT-TOLERANCE SUITE (tổng thời gian: $((SUITE_END - SUITE_START))s)"
echo "=========================================================="
OVERALL_PASS=true
{
    echo "{"
    echo "  \"timestamp\": \"$(date -Iseconds)\","
    echo "  \"nodes\": [ $(printf '"%s",' "${ACTIVE_NODES[@]}" | sed 's/,$//') ],"
    echo "  \"f_tolerance\": ${F_TOLERANCE},"
    echo "  \"scenarios\": {"
    for name in "${SCENARIO_LIST[@]}"; do
        status="${RESULT_STATUS[$name]:-SKIPPED}"
        [ "$status" == "FAIL" ] && OVERALL_PASS=false
    done
    for name in "${SCENARIO_LIST[@]}"; do
        status="${RESULT_STATUS[$name]:-SKIPPED}"
        detail="${RESULT_DETAIL[$name]:-not run}"
        comma=","
        [ "$name" == "${SCENARIO_LIST[-1]}" ] && comma=""
        printf '    "%s": {"status": "%s", "detail": "%s"}%s\n' "$name" "$status" "$detail" "$comma"
    done
    echo "  },"
    if [ "$OVERALL_PASS" == "true" ]; then
        echo "  \"overall\": \"PASS\""
    else
        echo "  \"overall\": \"FAIL\""
    fi
    echo "}"
} > "$RESULTS_JSON"

for name in "${SCENARIO_LIST[@]}"; do
    status="${RESULT_STATUS[$name]:-SKIPPED}"
    detail="${RESULT_DETAIL[$name]:-not run}"
    icon="✅"
    [ "$status" == "FAIL" ] && icon="❌" && OVERALL_PASS=false
    [ "$status" == "SKIPPED" ] && icon="⚪"
    echo "   ${icon} ${name}: ${status} (${detail})"
done
echo "📄 Kết quả chi tiết (JSON): ${RESULTS_JSON}"
echo "=========================================================="

if [ "$OVERALL_PASS" == "true" ]; then
    echo "🏆 TẤT CẢ SCENARIO ĐỀU PASS!"
    exit 0
else
    echo "❌ CÓ SCENARIO THẤT BẠI — xem log phía trên để biết chi tiết."
    exit 1
fi
