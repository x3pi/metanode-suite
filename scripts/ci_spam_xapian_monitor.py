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
import analyze_tx_order

TEST_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
METANODE_DIR = os.path.join(os.path.dirname(os.path.dirname(TEST_SCRIPT_DIR)), "metanode")
TEST_SCRIPT = "./test-spam-xapian.sh"
TEST_SCRIPT_NO_START = "./test-spam-xapian-no-deploy.sh"
LOGS_DIR = os.path.join(TEST_SCRIPT_DIR, "spam_xapian_logs")

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
    processes_to_kill = [
        "block_hash_checker",
        "rpc-tcp-simple",
        "tps_blast_cc",
        "test-spam-xapian",
        "run_spam.sh",
        "spam_xapian_test"
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
        error_report = os.path.join(TEST_SCRIPT_DIR, "error_report.txt")
        if os.path.exists(error_report):
            os.remove(error_report)
        sentinel = "/tmp/MTN_CHAIN_ERROR_STOP"
        if os.path.exists(sentinel):
            os.remove(sentinel)

        if os.path.exists(LOGS_DIR):
            log_files = [os.path.join(LOGS_DIR, f) for f in os.listdir(LOGS_DIR) if f.endswith(".log")]
            log_files.sort(key=os.path.getmtime, reverse=True)
            if len(log_files) > 1:
                for f in log_files[1:]:
                    try:
                        os.remove(f)
                    except OSError:
                        pass
    except Exception as e:
        print(f"[{datetime.datetime.now()}] ⚠️ Lỗi khi dọn dẹp logs cũ: {e}")

def has_real_error(exit_code, log_file):
    if exit_code > 0:
        return True
    if os.path.exists("/tmp/MTN_CHAIN_ERROR_STOP"):
        return True
    return False

def main():
    os.environ["MTN_TELE_ALERT"] = "true"
    os.makedirs(LOGS_DIR, exist_ok=True)

    current_process = None
    no_listen = "--no-listen" in sys.argv
    if no_listen:
        sys.argv.remove("--no-listen")
    no_start = "--no-start" in sys.argv
    if no_start:
        sys.argv.remove("--no-start")
    selected_script = TEST_SCRIPT_NO_START if no_start else TEST_SCRIPT
    args = [selected_script] + sys.argv[1:]
    
    print(f"=======================================================")
    print(f"🚀 SPAM XAPIAN - GITHUB CI/CD MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"📜 Script thực thi: {selected_script}")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"=======================================================\n")
    
    last_commit = get_remote_commit()
    if not last_commit:
        last_commit = get_local_commit()

    clean_old_logs()
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = os.path.join(LOGS_DIR, f"spam_xapian_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")

    commit_short = last_commit[:8] if last_commit else "init"
    server_info = get_server_ip_info()
    send_telegram_message(f"🚀 *[SPAM XAPIAN]* BẮT ĐẦU CHẠY PIPELINE MỚI!\n\n*Server:* `{server_info}`\n*Commit:* `{commit_short}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`")

    with open(log_file, "w") as f:
        current_process = subprocess.Popen(
            args, cwd=TEST_SCRIPT_DIR, stdout=f, stderr=subprocess.STDOUT, preexec_fn=os.setsid
        )
    
    try:
        while True:
            try:
                time.sleep(10)
                
                # Chủ động LẮNG NGHE cờ báo lỗi từ block_hash_checker ngay cả khi Spam đang chạy
                if current_process and current_process.poll() is None:
                    if os.path.exists("/tmp/MTN_CHAIN_ERROR_STOP"):
                        print(f"[{datetime.datetime.now()}] 🚨 Phát hiện cờ dừng khẩn cấp từ hệ thống!")
                        kill_process_group(current_process)
                        time.sleep(1) # Nhường thời gian để process chết hẳn, vòng lặp sau sẽ bắt lấy exit_code

                if current_process and current_process.poll() is not None:
                    exit_code = current_process.poll()
                    commit_short = last_commit[:8] if last_commit else "N/A"
                    if has_real_error(exit_code, log_file):
                        tail_logs = "Không thể đọc log."
                        if os.path.exists(log_file):
                            with open(log_file, "r", errors="replace") as f:
                                tail_logs = "".join(f.readlines()[-50:])
                                if len(tail_logs) > 3800:
                                    tail_logs = tail_logs[-3800:]

                        err_details = ""
                        if os.path.exists("/tmp/MTN_CHAIN_ERROR_STOP"):
                            with open("/tmp/MTN_CHAIN_ERROR_STOP", "r", errors="replace") as f:
                                err_details = f.read().strip()
                                if len(err_details) > 1500:
                                    err_details = err_details[:1500] + "\n... (truncated)"

                        server_info = get_server_ip_info()
                        real_code = exit_code if exit_code > 0 else "Lỗi ngầm định"
                        
                        # Truncate tail_logs if it's too long
                        if len(tail_logs) > 1500:
                            tail_logs = tail_logs[-1500:]

                        msg = f"❌ *[SPAM XAPIAN]* CẢNH BÁO LỖI PIPELINE!\n\n*Server:* `{server_info}`\nBài test (Commit: `{commit_short}`) THẤT BẠI (Exit Code: `{real_code}`).\n\n"
                        if err_details:
                            msg += f"🚨 *Lỗi phát hiện:*\n```\n{err_details}\n```\n\n"
                            
                        # Phân tích thứ tự giao dịch
                        count, order_details = analyze_tx_order.parse_logs()
                        if count > 0:
                            msg += f"🚨 *Lỗi lệch thứ tự TX:*\n```\n{order_details}\n```\n\n"
                        else:
                            msg += f"✅ *Thứ tự TX:* Khớp nhau giữa các node.\n\n"

                        msg += f"📄 *Trích xuất log cuối:*\n```\n{tail_logs}\n```\n"
                        send_telegram_message(msg)
                    else:
                        server_info = get_server_ip_info()
                        msg = f"✅ *[SPAM XAPIAN]* HOÀN TẤT THÀNH CÔNG!\n\n*Server:* `{server_info}`\nBài test (Commit: `{commit_short}`) chạy ổn định không lỗi.\n\n"
                        
                        count, order_details = analyze_tx_order.parse_logs()
                        if count > 0:
                            msg += f"⚠️ *Cảnh báo: Phát hiện lệch thứ tự TX!*\n```\n{order_details}\n```\n"
                        else:
                            msg += f"✅ Thứ tự TX khớp nhau giữa tất cả các node."
                            
                        send_telegram_message(msg)

                    current_process = None
                    if no_listen:
                        if has_real_error(exit_code, log_file):
                            sys.exit(1)
                        else:
                            print(f"[{datetime.datetime.now()}] Cờ --no-listen được bật. Chạy xong 1 vòng không lỗi. Tiếp tục chạy vòng test mới...")
                            clean_up_orphans()
                            time.sleep(5)
                            clean_old_logs()
                            timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                            log_file = os.path.join(LOGS_DIR, f"spam_xapian_{last_commit[:8] if last_commit else 'init'}_{timestamp}.log")
                            server_info = get_server_ip_info()
                            send_telegram_message(f"🚀 *[SPAM XAPIAN]* TIẾP TỤC VÒNG SPAM MỚI (no-listen)!\n\n*Server:* `{server_info}`\n*Commit:* `{commit_short}`\n*Thời gian:* `{datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}`")
                            with open(log_file, "w") as f:
                                current_process = subprocess.Popen(
                                    args, cwd=TEST_SCRIPT_DIR, stdout=f, stderr=subprocess.STDOUT, preexec_fn=os.setsid
                                )
                            continue
                    
                if no_listen:
                    continue

                current_commit = get_remote_commit()
                if not current_commit or current_commit == last_commit:
                    continue
                    
                print(f"🔄 PHÁT HIỆN COMMIT MỚI! {current_commit}")
                last_commit = current_commit
                if not pull_latest_code():
                    continue
                
                if current_process and current_process.poll() is None:
                    kill_process_group(current_process)
                
                clean_up_orphans()
                time.sleep(5)
                clean_old_logs()
                
                timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
                log_file = os.path.join(LOGS_DIR, f"spam_xapian_{current_commit[:8]}_{timestamp}.log")
                server_info = get_server_ip_info()
                send_telegram_message(f"🚀 *[SPAM XAPIAN]* PHÁT HIỆN CODE MỚI!\n\n*Server:* `{server_info}`\n*Commit mới:* `{current_commit[:8]}`")
                
                with open(log_file, "w") as f:
                    current_process = subprocess.Popen(
                        args, cwd=TEST_SCRIPT_DIR, stdout=f, stderr=subprocess.STDOUT, preexec_fn=os.setsid
                    )
            except Exception as loop_err:
                print(f"[{datetime.datetime.now()}] ⚠️ Lỗi vòng lặp: {loop_err}")
                time.sleep(5)
    except KeyboardInterrupt:
        if current_process and current_process.poll() is None:
            kill_process_group(current_process)
        clean_up_orphans()

if __name__ == "__main__":
    main()
