#!/usr/bin/env python3
import os
import time
import subprocess
import signal
import datetime
import sys
import urllib.request
import urllib.parse

TEST_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
METANODE_DIR = os.path.join(os.path.dirname(os.path.dirname(TEST_SCRIPT_DIR)), "metanode")
TEST_SCRIPT = "./test-node-recovery-gap.sh"
LOGS_DIR = os.path.join(TEST_SCRIPT_DIR, "recovery_test_logs")

TELEGRAM_BOT_TOKEN = "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
TELEGRAM_CHAT_ID = "-1003867050625"

def send_telegram_message(message):
    try:
        url = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}/sendMessage"
        data = urllib.parse.urlencode({
            'chat_id': TELEGRAM_CHAT_ID,
            'text': message,
            'parse_mode': 'Markdown'
        }).encode('utf-8')
        req = urllib.request.Request(url, data=data)
        with urllib.request.urlopen(req, timeout=10) as response:
            pass
    except Exception as e:
        print(f"[{datetime.datetime.now()}] ⚠️ Error sending telegram message: {e}")

def get_remote_commit():
    try:
        result = subprocess.run(
            ["git", "ls-remote", "origin", "refs/heads/main"],
            cwd=METANODE_DIR,
            capture_output=True,
            text=True,
            check=True,
            timeout=10
        )
        output = result.stdout.strip()
        if output:
            return output.split()[0]
        return None
    except Exception as e:
        print(f"[{datetime.datetime.now()}] Error checking remote commit: {e}")
        return None

def pull_latest_code():
    try:
        print(f"[{datetime.datetime.now()}] 📥 Đang tải (pull) mã nguồn mới nhất từ GitHub...")
        result = subprocess.run(
            ["git", "pull", "origin", "main"],
            cwd=METANODE_DIR,
            capture_output=True,
            text=True,
            check=True,
            timeout=30
        )
        print(f"[{datetime.datetime.now()}] ✅ Kéo code thành công!")
        return True
    except subprocess.CalledProcessError as e:
        print(f"[{datetime.datetime.now()}] ❌ Lỗi khi kéo code: {e.stderr}")
        return False
    except subprocess.TimeoutExpired:
        print(f"[{datetime.datetime.now()}] ❌ Timeout khi kéo code")
        return False

def get_local_commit():
    try:
        result = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=METANODE_DIR,
            capture_output=True,
            text=True,
            check=True
        )
        return result.stdout.strip()
    except subprocess.CalledProcessError:
        return None

def clean_up_orphans():
    """Dọn dẹp các tiến trình có thể còn sót lại của bài test trước"""
    processes_to_kill = [
        "block_hash_checker",
        "rpc-tcp-simple",
        "tps_blast_cc",
        "test-node-recovery-gap"
    ]
    for proc in processes_to_kill:
        subprocess.run(["pkill", "-f", proc], capture_output=True)

def kill_process_group(process):
    if process:
        try:
            print(f"[{datetime.datetime.now()}] 🛑 Terminating previous test script (PID: {process.pid})")
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
            
            # Chờ dọn dẹp
            for _ in range(30):
                if process.poll() is not None:
                    break
                time.sleep(0.1)
                
            # Cưỡng chế kill nếu chưa chết
            if process.poll() is None:
                os.killpg(os.getpgid(process.pid), signal.SIGKILL)
                process.wait()
        except ProcessLookupError:
            pass
        except Exception as e:
            print(f"[{datetime.datetime.now()}] Error killing process: {e}")

def main():
    os.makedirs(LOGS_DIR, exist_ok=True)
    
    print(f"=======================================================")
    print(f"🚀 RECOVERY GAP - GITHUB CI/CD MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"📜 Script thực thi: {TEST_SCRIPT}")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"=======================================================\n")
    
    last_commit = get_remote_commit()
    if not last_commit:
        print(f"[{datetime.datetime.now()}] ⚠️ Không thể kết nối GitHub, đang fallback dùng commit local...")
        last_commit = get_local_commit()
        
    print(f"[{datetime.datetime.now()}] Baseline commit (Remote): {last_commit}")
    
    current_process = None
    args = [TEST_SCRIPT] + sys.argv[1:]
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = os.path.join(LOGS_DIR, f"recovery_test_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")
    
    print(f"[{datetime.datetime.now()}] ▶️ Đang chạy bài test đầu tiên. Ghi log ra: {log_file}")
    
    commit_short = last_commit[:8] if last_commit else "init"
    send_telegram_message(f"🚀 *[CI Monitor]* BẮT ĐẦU RECOVERY TEST PIPELINE MỚI!\n\n*Commit:* `{commit_short}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`\n*Lý do:* Khởi động CI Monitor")
    
    with open(log_file, "w") as f:
        current_process = subprocess.Popen(
            args,
            cwd=TEST_SCRIPT_DIR,
            stdout=f,
            stderr=subprocess.STDOUT,
            preexec_fn=os.setsid
        )
    
    try:
        while True:
            try:
                time.sleep(10) # Chu kỳ check github
                
                # 1. Kiểm tra tiến trình hiện tại có chết không
                if current_process and current_process.poll() is not None:
                    exit_code = current_process.poll()
                    print(f"[{datetime.datetime.now()}] 🏁 Bài test hiện tại đã xong (Exit Code: {exit_code}). Đang chờ commit mới...")
                    
                    if exit_code > 0:
                        commit_short = last_commit[:8] if last_commit else "N/A"
                        tail_logs = "Không thể đọc file log."
                        try:
                            if os.path.exists(log_file):
                                with open(log_file, "r") as f:
                                    lines = f.readlines()
                                    tail_logs = "".join(lines[-25:])
                                    if len(tail_logs) > 3000:
                                        tail_logs = tail_logs[-3000:]
                        except Exception as e:
                            print(f"Lỗi đọc log: {e}")
                            
                        msg = f"❌ *[CI Monitor]* CẢNH BÁO LỖI RECOVERY PIPELINE!\n\nBài test (Commit: `{commit_short}`) THẤT BẠI với Exit Code: `{exit_code}`.\n\n📄 *Trích xuất log cuối:*\n```\n{tail_logs}\n```\n\nHãy kiểm tra log chi tiết trên Server."
                        send_telegram_message(msg)
                    else:
                        commit_short = last_commit[:8] if last_commit else "N/A"
                        msg = f"✅ *[CI Monitor]* RECOVERY TEST HOÀN TẤT THÀNH CÔNG!\n\nBài test (Commit: `{commit_short}`) chạy mượt mà không gặp lỗi phân nhánh hay lệch hash."
                        send_telegram_message(msg)

                    current_process = None
                    
                # 2. Kiểm tra commit mới trên GitHub
                current_commit = get_remote_commit()
                if not current_commit:
                    continue
                    
                if current_commit != last_commit:
                    print(f"\n=======================================================")
                    print(f"[{datetime.datetime.now()}] 🔄 PHÁT HIỆN COMMIT MỚI TRÊN GITHUB!")
                    print(f"   Mã cũ (remote): {last_commit}")
                    print(f"   Mã mới (remote): {current_commit}")
                    print(f"=======================================================\n")
                    
                    last_commit = current_commit
                    
                    pull_success = pull_latest_code()
                    if not pull_success:
                        print(f"[{datetime.datetime.now()}] ⚠️ Bỏ qua chạy test do Pull code thất bại.")
                        continue
                    
                    if current_process and current_process.poll() is None:
                        kill_process_group(current_process)
                    
                    clean_up_orphans()
                    print(f"[{datetime.datetime.now()}] ⏳ Đang đợi 5 giây để đóng hoàn toàn các port cũ...")
                    time.sleep(5)
                    
                    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                    log_file = os.path.join(LOGS_DIR, f"recovery_test_{current_commit[:8]}_{timestamp}.log")
                    print(f"[{datetime.datetime.now()}] ▶️ Chạy bài test RECOVERY MỚI. Ghi log ra: {log_file}")
                    
                    send_telegram_message(f"🚀 *[CI Monitor]* PHÁT HIỆN CODE MỚI, KHỞI ĐỘNG RECOVERY PIPELINE!\n\n*Commit mới:* `{current_commit[:8]}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`")
                    
                    with open(log_file, "w") as f:
                        current_process = subprocess.Popen(
                            args,
                            cwd=TEST_SCRIPT_DIR,
                            stdout=f,
                            stderr=subprocess.STDOUT,
                            preexec_fn=os.setsid
                        )
            except Exception as loop_err:
                print(f"[{datetime.datetime.now()}] ⚠️ Lỗi trong vòng lặp chính của Monitor: {loop_err}")
                time.sleep(5)
                
    except KeyboardInterrupt:
        print(f"\n[{datetime.datetime.now()}] 🛑 Đang dừng chương trình theo dõi...")
        if current_process and current_process.poll() is None:
            kill_process_group(current_process)
        clean_up_orphans()
        print("Đã tắt hoàn toàn.")

if __name__ == "__main__":
    main()
