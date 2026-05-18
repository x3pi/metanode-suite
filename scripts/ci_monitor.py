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
TEST_SCRIPT = "./auto_test.sh"
LOGS_DIR = os.path.join(TEST_SCRIPT_DIR, "auto_test_logs")

TELEGRAM_BOT_TOKEN = "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
TELEGRAM_CHAT_ID = "-1003867050625"

def send_telegram_message(message):
    try:
        url = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}/sendMessage"
        data = urllib.parse.urlencode({
            'chat_id': TELEGRAM_CHAT_ID,
            'text': message
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

def main():
    os.makedirs(LOGS_DIR, exist_ok=True)
    
    print(f"=======================================================")
    print(f"🚀 METANODE 24/7 GITHUB CI/CD MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"🌐 Remote branch: origin/main (GitHub)")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"=======================================================\n")
    
    # Khởi tạo commit ban đầu từ remote
    last_commit = get_remote_commit()
    if not last_commit:
        print(f"[{datetime.datetime.now()}] Không thể kết nối GitHub, đang fallback dùng commit local...")
        last_commit = get_local_commit()
        
    print(f"[{datetime.datetime.now()}] Baseline commit (Remote): {last_commit}")
    
    current_process = None
    args = [TEST_SCRIPT] + sys.argv[1:]
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = os.path.join(LOGS_DIR, f"test_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")
    
    print(f"[{datetime.datetime.now()}] Đang chạy bài test đầu tiên. Ghi log ra: {log_file}")
    
    commit_short = last_commit[:8] if last_commit else "init"
    send_telegram_message(f"🚀 [CI Monitor] BẮT ĐẦU CHẠY PIPELINE MỚI!\n\nCommit: {commit_short}\nThời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}\nLý do: Khởi động CI Monitor")
    
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
            time.sleep(10) # Kiểm tra GitHub mỗi 10 giây (giảm tải cho mạng)
            
            if current_process and current_process.poll() is not None:
                exit_code = current_process.poll()
                print(f"[{datetime.datetime.now()}] Bài test hiện tại đã xong (Exit Code: {exit_code}). Đang chờ commit mới...")
                
                # Cảnh báo lỗi qua Telegram nếu script chết với mã lỗi > 0 (không phải do bị kill bởi tín hiệu)
                if exit_code > 0:
                    commit_short = last_commit[:8] if last_commit else "N/A"
                    msg = f"❌ [CI Monitor] CẢNH BÁO LỖI PIPELINE!\n\nBài test tự động cho nhánh main (Commit: {commit_short}) vừa THẤT BẠI với Exit Code: {exit_code}.\n\nHãy kiểm tra file log trên server."
                    send_telegram_message(msg)
                
                current_process = None
                
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
                
                # Cập nhật mã hash để không lặp lại
                last_commit = current_commit
                
                # Kéo code mới về máy
                pull_success = pull_latest_code()
                if not pull_success:
                    print(f"[{datetime.datetime.now()}] Bỏ qua chạy test do Pull code thất bại.")
                    continue
                
                # Kill bài test cũ (nếu đang chạy)
                if current_process and current_process.poll() is None:
                    kill_process_group(current_process)
                
                # Dọn dẹp tiến trình
                subprocess.run(["pkill", "-f", "block_hash_checker"], capture_output=True)
                print(f"[{datetime.datetime.now()}] Đang đợi 5 giây để đóng hoàn toàn các port cũ...")
                time.sleep(5)
                
                # (Việc build lại code sẽ do auto_test.sh đảm nhiệm vì có tham số --build-all)
                
                # Bắt đầu bài test mới
                timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                log_file = os.path.join(LOGS_DIR, f"test_{current_commit[:8]}_{timestamp}.log")
                print(f"[{datetime.datetime.now()}] Chạy bài test MỚI. Ghi log ra: {log_file}")
                
                send_telegram_message(f"🚀 [CI Monitor] BẮT ĐẦU CHẠY PIPELINE MỚI!\n\nCommit: {current_commit[:8]}\nThời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}\nLý do: Phát hiện mã mới trên GitHub")
                
                with open(log_file, "w") as f:
                    current_process = subprocess.Popen(
                        args,
                        cwd=TEST_SCRIPT_DIR,
                        stdout=f,
                        stderr=subprocess.STDOUT,
                        preexec_fn=os.setsid
                    )
                    
    except KeyboardInterrupt:
        print(f"\n[{datetime.datetime.now()}] Đang dừng chương trình theo dõi...")
        if current_process and current_process.poll() is None:
            kill_process_group(current_process)
        print("Đã tắt hoàn toàn.")

if __name__ == "__main__":
    main()
