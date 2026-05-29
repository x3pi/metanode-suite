#!/usr/bin/env python3
import os
import time
import subprocess
import signal
import datetime
import sys
import urllib.request
import urllib.parse
import socket

TEST_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
METANODE_DIR = os.path.join(os.path.dirname(os.path.dirname(TEST_SCRIPT_DIR)), "metanode")
TEST_SCRIPT = "./test-node-recovery-gap.sh"
LOGS_DIR = os.path.join(TEST_SCRIPT_DIR, "recovery_test_logs")

TELEGRAM_BOT_TOKEN = "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
TELEGRAM_CHAT_ID = "-1003867050625"

_SERVER_IP_INFO_CACHE = None

def get_server_ip_info():
    global _SERVER_IP_INFO_CACHE
    if _SERVER_IP_INFO_CACHE is not None:
        return _SERVER_IP_INFO_CACHE

    local_ip = "Unknown"
    hostname = "Unknown"
    try:
        hostname = socket.gethostname()
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        local_ip = s.getsockname()[0]
        s.close()
    except Exception:
        pass

    configured_ips = []
    try:
        result = subprocess.run(["hostname", "-I"], capture_output=True, text=True, timeout=1)
        configured_ips = [ip for ip in result.stdout.strip().split() if ip != "127.0.0.1"]
    except Exception:
        pass

    if configured_ips:
        ip_str = ", ".join(configured_ips)
    else:
        ip_str = local_ip

    public_ip = None
    try:
        req = urllib.request.Request("https://api.ipify.org", headers={'User-Agent': 'Mozilla/5.0'})
        with urllib.request.urlopen(req, timeout=2) as resp:
            public_ip = resp.read().decode('utf-8').strip()
    except Exception:
        pass

    if public_ip:
        _SERVER_IP_INFO_CACHE = f"{hostname} (Static/Private IP: {ip_str}, Public IP: {public_ip})"
    else:
        _SERVER_IP_INFO_CACHE = f"{hostname} (Static/Private IP: {ip_str})"
        
    return _SERVER_IP_INFO_CACHE


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
            check=True
        )
        output = result.stdout.strip()
        if output:
            return output.split()[0]
        return None
    except subprocess.CalledProcessError as e:
        print(f"[{datetime.datetime.now()}] Error checking remote commit: {e}")
        return None

def pull_latest_code():
    try:
        print(f"[{datetime.datetime.now()}] Đang tải (pull) mã nguồn mới nhất từ GitHub...")
        result = subprocess.run(
            ["git", "pull", "origin", "main"],
            cwd=METANODE_DIR,
            capture_output=True,
            text=True,
            check=True
        )
        print(f"[{datetime.datetime.now()}] Kéo code thành công!")
        return True
    except subprocess.CalledProcessError as e:
        print(f"[{datetime.datetime.now()}] ❌ Lỗi khi kéo code: {e.stderr}")
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
            print(f"[{datetime.datetime.now()}] Terminating previous test script (PID: {process.pid})")
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
            
            for _ in range(30):
                if process.poll() is not None:
                    break
                time.sleep(0.1)
                
            if process.poll() is None:
                os.killpg(os.getpgid(process.pid), signal.SIGKILL)
                process.wait()
        except ProcessLookupError:
            pass
        except Exception as e:
            print(f"[{datetime.datetime.now()}] Error killing process: {e}")

def clean_old_logs():
    try:
        print(f"[{datetime.datetime.now()}] 🧹 Đang dọn dẹp logs cũ trước khi chạy build mới...")
        # Xóa error_report.txt cũ nếu có
        error_report = os.path.join(TEST_SCRIPT_DIR, "error_report.txt")
        if os.path.exists(error_report):
            os.remove(error_report)
            print(f"[{datetime.datetime.now()}]   → Đã xóa: error_report.txt")

        # Xóa file sentinel lỗi nếu còn sót
        sentinel = "/tmp/MTN_CHAIN_ERROR_STOP"
        if os.path.exists(sentinel):
            os.remove(sentinel)
            print(f"[{datetime.datetime.now()}]   → Đã xóa: {sentinel}")

        if os.path.exists(LOGS_DIR):
            log_files = [
                os.path.join(LOGS_DIR, f)
                for f in os.listdir(LOGS_DIR)
                if f.endswith(".log")
            ]
            log_files.sort(key=os.path.getmtime, reverse=True)

            if len(log_files) > 1:
                files_to_delete = log_files[1:]
                for f in files_to_delete:
                    try:
                        os.remove(f)
                    except OSError as e:
                        print(f"[{datetime.datetime.now()}]   ⚠️ Không thể xóa {f}: {e}")
                print(f"[{datetime.datetime.now()}]   → Đã dọn sạch {len(files_to_delete)} files log cũ, giữ lại 1 file mới nhất.")
            elif len(log_files) == 1:
                print(f"[{datetime.datetime.now()}]   → Chỉ có 1 file log cũ, đã giữ lại.")
            else:
                print(f"[{datetime.datetime.now()}]   → Thư mục logs trống.")
    except Exception as e:
        print(f"[{datetime.datetime.now()}] ⚠️ Lỗi khi dọn dẹp logs cũ: {e}")


def has_real_error(exit_code, log_file):
    """Kiểm tra lỗi thực sự: exit_code > 0 HOẶC file sentinel tồn tại.
    Lưu ý: simple_test.sh đã được fix để preserve exit code đúng.
    Sentinel /tmp/MTN_CHAIN_ERROR_STOP chặt được lỗi từ monitor_error_flag (test-node-recovery-gap.sh).
    """
    if exit_code > 0:
        return True
    # Kiểm tra file sentinel do script shell tạo ra khi có lỗi nghiêm trọng từ monitor_error_flag
    if os.path.exists("/tmp/MTN_CHAIN_ERROR_STOP"):
        print(f"[{datetime.datetime.now()}] ⚠️ Exit code=0 nhưng phát hiện file sentinel /tmp/MTN_CHAIN_ERROR_STOP!")
        return True
    return False

def main():
    os.environ["MTN_TELE_ALERT"] = "true"
    os.makedirs(LOGS_DIR, exist_ok=True)
    
    print(f"=======================================================")
    print(f"🚀 RECOVERY GAP - GITHUB CI/CD MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"📜 Script thực thi: {TEST_SCRIPT}")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"=======================================================\n")
    
    last_commit = get_remote_commit()
    if not last_commit:
        print(f"[{datetime.datetime.now()}] Không thể kết nối GitHub, đang fallback dùng commit local...")
        last_commit = get_local_commit()
        
    print(f"[{datetime.datetime.now()}] Baseline commit (Remote): {last_commit}")
    
    current_process = None
    
    no_listen = False
    if "--no-listen" in sys.argv:
        no_listen = True
        sys.argv.remove("--no-listen")

    args = [TEST_SCRIPT] + sys.argv[1:]

    # Dọn log cũ ngay cả trước lần chạy test đầu tiên
    clean_old_logs()

    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = os.path.join(LOGS_DIR, f"recovery_test_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")

    print(f"[{datetime.datetime.now()}] Đang chạy bài test đầu tiên. Ghi log ra: {log_file}")

    commit_short = last_commit[:8] if last_commit else "init"
    server_info = get_server_ip_info()
    send_telegram_message(f"🚀 *[RECOVERY TEST]* BẮT ĐẦU CHẠY PIPELINE MỚI!\n\n*Server:* `{server_info}`\n*Commit:* `{commit_short}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`\n*Lý do:* Khởi động CI Monitor")

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
                time.sleep(10) # Kiểm tra GitHub mỗi 10 giây
                
                if current_process and current_process.poll() is not None:
                    exit_code = current_process.poll()
                    print(f"[{datetime.datetime.now()}] Bài test hiện tại đã xong (Exit Code: {exit_code}). Đang chờ commit mới...")

                    commit_short = last_commit[:8] if last_commit else "N/A"
                    # Kiểm tra lỗi thực sự: exit_code > 0 HOẶC sentinel/log cho thấy lỗi
                    if has_real_error(exit_code, log_file):
                        tail_logs = "Không thể đọc file log."
                        try:
                            if os.path.exists(log_file):
                                with open(log_file, "r", errors="replace") as f:
                                    lines = f.readlines()
                                    tail_logs = "".join(lines[-50:])
                                    if len(tail_logs) > 3800:
                                        tail_logs = tail_logs[-3800:]
                        except Exception as e:
                            print(f"Lỗi đọc log: {e}")

                        # Check for specific data integrity error details
                        integrity_error_details = ""
                        sentinel_path = "/tmp/MTN_CHAIN_ERROR_STOP"
                        if os.path.exists(sentinel_path):
                            try:
                                with open(sentinel_path, "r", errors="replace") as f:
                                    integrity_error_details = f.read().strip()
                            except Exception as e:
                                print(f"Lỗi đọc sentinel: {e}")

                        server_info = get_server_ip_info()
                        real_code = exit_code if exit_code > 0 else "0 (lỗi phát hiện qua log/sentinel)"
                        
                        if integrity_error_details:
                            msg = f"❌ *[RECOVERY TEST]* CẢNH BÁO LỖI PIPELINE - LỖI DỮ LIỆU CẦN PHỤC HỒI!\n\n*Server:* `{server_info}`\nBài test (Commit: `{commit_short}`) THẤT BẠI (Exit Code: `{real_code}`).\n\n🚨 *Lỗi dữ liệu phát hiện:* `{integrity_error_details}`\n\n📄 *Trích xuất log cuối:*\n```\n{tail_logs}\n```\n\n👉 *Hướng khắc phục:* Khởi động lại bằng cách tải snapshot và chạy lại node."
                        else:
                            msg = f"❌ *[RECOVERY TEST]* CẢNH BÁO LỖI PIPELINE!\n\n*Server:* `{server_info}`\nBài test (Commit: `{commit_short}`) THẤT BẠI (Exit Code: `{real_code}`).\n\n📄 *Trích xuất log cuối:*\n```\n{tail_logs}\n```\n\nHãy kiểm tra log chi tiết trên Server."
                        
                        send_telegram_message(msg)
                    else:
                        server_info = get_server_ip_info()
                        msg = f"✅ *[RECOVERY TEST]* HOÀN TẤT THÀNH CÔNG!\n\n*Server:* `{server_info}`\nBài test (Commit: `{commit_short}`) chạy mượt mà không gặp lỗi phân nhánh hay lệch hash."
                        send_telegram_message(msg)

                    current_process = None
                    
                    if no_listen:
                        print(f"[{datetime.datetime.now()}] Cờ --no-listen được bật. Chạy xong 1 lần, kết thúc.")
                        break
                    
                if no_listen:
                    continue

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
                        print(f"[{datetime.datetime.now()}] Bỏ qua chạy test do Pull code thất bại.")
                        continue
                    
                    if current_process and current_process.poll() is None:
                        kill_process_group(current_process)
                    
                    clean_up_orphans()
                    print(f"[{datetime.datetime.now()}] Đang đợi 5 giây để đóng hoàn toàn các port cũ...")
                    time.sleep(5)
                    
                    clean_old_logs()
                    
                    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                    log_file = os.path.join(LOGS_DIR, f"recovery_test_{current_commit[:8]}_{timestamp}.log")
                    print(f"[{datetime.datetime.now()}] Chạy bài test RECOVERY MỚI. Ghi log ra: {log_file}")
                    
                    server_info = get_server_ip_info()
                    send_telegram_message(f"🚀 *[RECOVERY TEST]* PHÁT HIỆN CODE MỚI!\n\n*Server:* `{server_info}`\n*Commit mới:* `{current_commit[:8]}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`")
                    
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
        print(f"\n[{datetime.datetime.now()}] Đang dừng chương trình theo dõi...")
        if current_process and current_process.poll() is None:
            kill_process_group(current_process)
        clean_up_orphans()
        print("Đã tắt hoàn toàn.")

if __name__ == "__main__":
    main()
