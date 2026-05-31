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
TEST_SCRIPT = "./auto_test.sh"
LOGS_DIR = os.path.join(TEST_SCRIPT_DIR, "auto_test_logs")

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
        # Lấy tất cả các IP tĩnh được cấu hình cố định trên các card mạng của máy
        result = subprocess.run(["hostname", "-I"], capture_output=True, text=True, timeout=1)
        configured_ips = [ip for ip in result.stdout.strip().split() if ip != "127.0.0.1"]
    except Exception:
        pass

    # Nếu có IP tĩnh từ hostname -I, hiển thị danh sách này, nếu không fallback dùng local_ip
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
        with urllib.request.urlopen(req) as response:
            pass
    except Exception as e:
        print(f"[{datetime.datetime.now()}] ⚠️ Error sending telegram message: {e}")

def get_remote_commit():
    try:
        # Dùng ls-remote để lấy commit mới nhất từ server GitHub mà không cần fetch toàn bộ repo
        result = subprocess.run(
            ["git", "ls-remote", "origin", "refs/heads/main"],
            cwd=METANODE_DIR,
            capture_output=True,
            text=True,
            check=True
        )
        output = result.stdout.strip()
        if output:
            return output.split()[0] # Lấy mã hash đầu tiên
        return None
    except subprocess.CalledProcessError as e:
        print(f"[{datetime.datetime.now()}] Error checking remote commit: {e}")
        return None

def pull_latest_code():
    try:
        print(f"[{datetime.datetime.now()}] Đang tải (pull) mã nguồn mới nhất từ GitHub...")
        # Kéo code mới về và ghi đè an toàn
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

def kill_process_group(process):
    if process:
        try:
            print(f"[{datetime.datetime.now()}] Terminating previous auto_test.sh (PID: {process.pid})")
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
            
            # Cho tiến trình 3 giây để dọn dẹp
            for _ in range(30):
                if process.poll() is not None:
                    break
                time.sleep(0.1)
                
            # Nếu vẫn chưa chết, dùng SIGKILL
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
    if os.path.exists("/tmp/MTN_CHAIN_ERROR_STOP"):
        print(f"[{datetime.datetime.now()}] ⚠️ Exit code=0 nhưng phát hiện file sentinel /tmp/MTN_CHAIN_ERROR_STOP!")
        return True
    return False

def main():
    os.environ["MTN_TELE_ALERT"] = "true"
    os.makedirs(LOGS_DIR, exist_ok=True)
    
    batch_size = "Default"
    if "--batch" in sys.argv:
        try:
            batch_idx = sys.argv.index("--batch")
            batch_size = sys.argv[batch_idx + 1]
        except Exception:
            pass

    print(f"=======================================================")
    print(f"🚀 METANODE 24/7 GITHUB CI/CD MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"🌐 Remote branch: origin/main (GitHub)")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"⚙️  Batch Size: {batch_size}")
    print(f"=======================================================\n")
    
    # Khởi tạo commit ban đầu từ remote
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
    log_file = os.path.join(LOGS_DIR, f"test_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")

    print(f"[{datetime.datetime.now()}] Đang chạy bài test đầu tiên. Ghi log ra: {log_file}")

    commit_short = last_commit[:8] if last_commit else "init"
    server_info = get_server_ip_info()
    send_telegram_message(f"🚀 [CI Monitor - BATCH: {batch_size}] BẮT ĐẦU CHẠY PIPELINE MỚI!\n\nServer: {server_info}\nCommit: {commit_short}\nThời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}\nLý do: Khởi động CI Monitor")

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
                time.sleep(10) # Kiểm tra GitHub mỗi 10 giây (giảm tải cho mạng)
                
                if current_process and current_process.poll() is not None:
                    exit_code = current_process.poll()
                    print(f"[{datetime.datetime.now()}] Bài test hiện tại đã xong (Exit Code: {exit_code}). Đang chờ commit mới...")

                    commit_short = last_commit[:8] if last_commit else "N/A"
                    # Cảnh báo lỗi qua Telegram nếu script chết với mã lỗi > 0 hoặc phát hiện lỗi qua sentinel/log
                    if has_real_error(exit_code, log_file):
                        # Đọc 25 dòng cuối của file log để gửi kèm
                        tail_logs = "Không thể đọc file log."
                        try:
                            if os.path.exists(log_file):
                                with open(log_file, "r", errors="replace") as f:
                                    lines = f.readlines()
                                    tail_logs = "".join(lines[-50:])
                                    # Giới hạn số lượng ký tự để không vượt quá giới hạn của Telegram
                                    if len(tail_logs) > 3800:
                                        tail_logs = tail_logs[-3800:]
                        except Exception as e:
                            print(f"Lỗi đọc log: {e}")

                        server_info = get_server_ip_info()
                        real_code = exit_code if exit_code > 0 else "0 (lỗi phát hiện qua log/sentinel)"
                        msg = f"❌ [CI Monitor - BATCH: {batch_size}] CẢNH BÁO LỖI PIPELINE!\n\nServer: {server_info}\nBài test (Commit: {commit_short}) THẤT BẠI (Exit Code: {real_code}).\n\n📄 *Trích xuất 50 dòng log cuối:*\n```\n{tail_logs}\n```\n\nHãy kiểm tra file log đầy đủ trên server."
                        send_telegram_message(msg)
                    else:
                        server_info = get_server_ip_info()
                        msg = f"✅ [CI Monitor - BATCH: {batch_size}] HOÀN TẤT THÀNH CÔNG!\n\nServer: {server_info}\nBài test (Commit: {commit_short}) chạy mượt mà không gặp lỗi phân nhánh hay lệch hash."
                        send_telegram_message(msg)

                    current_process = None
                    
                    if no_listen:
                        if has_real_error(exit_code, log_file):
                            sys.exit(1)
                        else:
                            print(f"[{datetime.datetime.now()}] Cờ --no-listen được bật. Chạy xong 1 vòng không lỗi. Tiếp tục chạy vòng test mới...")
                            subprocess.run(["pkill", "-f", "block_hash_checker"], capture_output=True)
                            print(f"[{datetime.datetime.now()}] Đang đợi 5 giây để đóng hoàn toàn các port cũ...")
                            time.sleep(5)
                            clean_old_logs()
                            timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                            log_file = os.path.join(LOGS_DIR, f"test_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")
                            server_info = get_server_ip_info()
                            send_telegram_message(f"🚀 [CI Monitor - BATCH: {batch_size}] TIẾP TỤC VÒNG SPAM MỚI (no-listen)!\n\nServer: {server_info}\nCommit: {commit_short}\nThời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}")
                            with open(log_file, "w") as f:
                                current_process = subprocess.Popen(
                                    args,
                                    cwd=TEST_SCRIPT_DIR,
                                    stdout=f,
                                    stderr=subprocess.STDOUT,
                                    preexec_fn=os.setsid
                                )
                            continue
                    
                if no_listen:
                    continue

                current_commit = get_remote_commit()
                
                # Bỏ qua nếu lỗi mạng không lấy được commit
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
                    
                    subprocess.run(["pkill", "-f", "block_hash_checker"], capture_output=True)
                    print(f"[{datetime.datetime.now()}] Đang đợi 5 giây để đóng hoàn toàn các port cũ...")
                    time.sleep(5)
                    
                    # Dọn dẹp logs cũ trước khi chạy build/test mới
                    clean_old_logs()
                    
                    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                    log_file = os.path.join(LOGS_DIR, f"test_{current_commit[:8]}_{timestamp}.log")
                    print(f"[{datetime.datetime.now()}] Chạy bài test MỚI. Ghi log ra: {log_file}")
                    
                    server_info = get_server_ip_info()
                    send_telegram_message(f"🚀 [CI Monitor - BATCH: {batch_size}] BẮT ĐẦU CHẠY PIPELINE MỚI!\n\nServer: {server_info}\nCommit: {current_commit[:8]}\nThời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}\nLý do: Phát hiện mã mới trên GitHub")
                    
                    with open(log_file, "w") as f:
                        current_process = subprocess.Popen(
                            args,
                            cwd=TEST_SCRIPT_DIR,
                            stdout=f,
                            stderr=subprocess.STDOUT,
                            preexec_fn=os.setsid
                        )
            except Exception as loop_err:
                print(f"[{datetime.datetime.now()}] ⚠️ Lỗi trong vòng lặp chính của Monitor (Đã bắt lỗi để tránh crash): {loop_err}")
                time.sleep(5)
                
    except KeyboardInterrupt:
        print(f"\n[{datetime.datetime.now()}] Đang dừng chương trình theo dõi...")
        if current_process and current_process.poll() is None:
            kill_process_group(current_process)
        print("Đã tắt hoàn toàn.")

if __name__ == "__main__":
    main()
