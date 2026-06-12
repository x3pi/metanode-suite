#!/usr/bin/env python3
import os
import threading
import json
import time
import subprocess
import datetime
import urllib.request
import urllib.parse
import socket

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
METANODE_DIR = os.path.join(os.path.dirname(os.path.dirname(SCRIPT_DIR)), "metanode")
DEPLOY_SCRIPT_DIR = os.path.join(METANODE_DIR, "consensus", "metanode", "scripts", "node")
DEPLOY_CMD = ["./deploy_systemd_cluster.sh", "--env", "deploy-muti-node.env", "--all"]
LOGS_DIR = os.path.join(SCRIPT_DIR, "auto_update_logs")

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
        try:
            with urllib.request.urlopen(req, timeout=10) as response:
                pass
        except Exception as e:
            if getattr(e, 'code', None) == 400:
                data_plain = urllib.parse.urlencode({
                    'chat_id': TELEGRAM_CHAT_ID,
                    'text': message.replace('```', '')
                }).encode('utf-8')
                req_plain = urllib.request.Request(url, data=data_plain)
                with urllib.request.urlopen(req_plain, timeout=10) as response_plain:
                    pass
            else:
                raise e
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

def clean_old_logs():
    try:
        print(f"[{datetime.datetime.now()}] 🧹 Đang dọn dẹp logs cũ...")
        if os.path.exists(LOGS_DIR):
            log_files = [os.path.join(LOGS_DIR, f) for f in os.listdir(LOGS_DIR) if f.endswith(".log")]
            log_files.sort(key=os.path.getmtime, reverse=True)

            if len(log_files) > 5:  # Giữ lại 5 file log gần nhất
                files_to_delete = log_files[5:]
                for f in files_to_delete:
                    try:
                        os.remove(f)
                    except OSError:
                        pass
                print(f"[{datetime.datetime.now()}]   → Đã dọn sạch {len(files_to_delete)} files log cũ.")
    except Exception as e:
        print(f"[{datetime.datetime.now()}] ⚠️ Lỗi khi dọn dẹp logs cũ: {e}")

def run_deployment(commit_short):
    clean_old_logs()
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = os.path.join(LOGS_DIR, f"update_{commit_short}_{timestamp}.log")
    
    print(f"[{datetime.datetime.now()}] Đang chạy lệnh deploy update. Ghi log ra: {log_file}")
    
    try:
        with open(log_file, "w") as f:
            process = subprocess.run(
                DEPLOY_CMD,
                cwd=DEPLOY_SCRIPT_DIR,
                stdout=f,
                stderr=subprocess.STDOUT
            )
        return process.returncode, log_file
    except Exception as e:
        print(f"[{datetime.datetime.now()}] Lỗi khi chạy lệnh deploy: {e}")
        return -1, log_file

def main():
    print(f"[{datetime.datetime.now()}] Đang khởi động các monitors ngầm...")
    subprocess.Popen(["./start_monitors.sh"], cwd=os.path.dirname(os.path.abspath(__file__)))
    os.makedirs(LOGS_DIR, exist_ok=True)
    
    print(f"=======================================================")
    print(f"🚀 METANODE 24/7 GITHUB AUTO-UPDATE MONITOR")
    print(f"📂 Theo dõi repo: {METANODE_DIR}")
    print(f"🌐 Remote branch: origin/main (GitHub)")
    print(f"📂 Thư mục Logs: {LOGS_DIR}")
    print(f"⚙️  Lệnh Deploy: {' '.join(DEPLOY_CMD)}")
    print(f"=======================================================\n")
    
    last_commit = get_remote_commit()
    if not last_commit:
        print(f"[{datetime.datetime.now()}] Không thể kết nối GitHub, đang fallback dùng commit local...")
        last_commit = get_local_commit()
        
    print(f"[{datetime.datetime.now()}] Baseline commit (Remote): {last_commit}")
    
    server_info = get_server_ip_info()
    msg = (f"🌌 𝗔𝗨𝗧𝗢-𝗨𝗣𝗗𝗔𝗧𝗘𝗥 𝗦𝗬𝗦𝗧𝗘𝗠 \n"
           f"━━━━━━━━━━━━━━━━━━━━━\n"
           f"🔥 KHỞI ĐỘNG & GIÁM SÁT CỤM NODE \n"
           f"📡 Server: {server_info}\n"
           f"🔖 Commit hiện tại: {last_commit[:8] if last_commit else 'Unknown'}\n"
           f"⏱ Thời gian: {datetime.datetime.now().strftime('%H:%M:%S %d/%m/%Y')}\n"
           f"━━━━━━━━━━━━━━━━━━━━━\n"
           f"⚡️ Hệ thống đang giám sát GitHub 24/7...")
    send_telegram_message(msg)
    
    try:
        while True:
            try:
                time.sleep(10) # Kiểm tra GitHub mỗi 10 giây
                
                current_commit = get_remote_commit()
                if not current_commit:
                    continue
                    
                if current_commit != last_commit:
                    print(f"\n=======================================================")
                    print(f"[{datetime.datetime.now()}] 🔄 PHÁT HIỆN COMMIT MỚI TRÊN GITHUB!")
                    print(f"   Mã cũ: {last_commit}")
                    print(f"   Mã mới: {current_commit}")
                    print(f"=======================================================\n")
                    
                    last_commit = current_commit
                    commit_short = current_commit[:8]
                    
                    pull_success = pull_latest_code()
                    if not pull_success:
                        print(f"[{datetime.datetime.now()}] Bỏ qua deploy do Pull code thất bại.")
                        send_telegram_message(f"❌ [Auto-Update] LỖI PULL CODE!\n\nServer: {server_info}\nCommit: {commit_short}\nKhông thể pull code mới từ GitHub.")
                        continue
                    
                    server_info = get_server_ip_info()
                    update_msg = (f"🔄 𝗣𝗛𝗔́𝗧 𝗛𝗜𝗘̣̂𝗡 𝗖𝗢𝗗𝗘 𝗠𝗢̛́𝗜 \n"
                                  f"━━━━━━━━━━━━━━━━━━━━━\n"
                                  f"📡 Server: {server_info}\n"
                                  f"🔖 Commit mới: {commit_short}\n"
                                  f"⚙️ Đang tiến hành Build & Deploy lại toàn bộ cụm Node...\n"
                                  f"*(Dữ liệu Blockchain cũ vẫn được giữ nguyên)*")
                    send_telegram_message(update_msg)
                    
                    # Chạy lệnh cập nhật cluster
                    exit_code, log_file = run_deployment(commit_short)
                    
                    if exit_code > 0:
                        tail_logs = "Không thể đọc file log."
                        try:
                            if os.path.exists(log_file):
                                with open(log_file, "r", errors="replace") as f:
                                    lines = f.readlines()
                                    tail_logs = "".join(lines[-50:])
                                    if len(tail_logs) > 3000:
                                        tail_logs = tail_logs[-3000:]
                        except Exception as e:
                            pass
                            
                        msg = f"❌ [Auto-Update] LỖI TRIỂN KHAI!\n\nServer: {server_info}\nCập nhật (Commit: {commit_short}) THẤT BẠI (Exit Code: {exit_code}).\n\n📄 *Trích xuất 50 dòng log cuối:*\n```\n{tail_logs}\n```\n\nHãy kiểm tra server."
                        send_telegram_message(msg)
                    else:
                        msg = (f"✅ 𝗧𝗥𝗜𝗘̂̉𝗡 𝗞𝗛𝗔𝗜 𝗧𝗛𝗔̀𝗡𝗛 𝗖𝗢̂𝗡𝗚 \n"
                               f"━━━━━━━━━━━━━━━━━━━━━\n"
                               f"📡 Server: {server_info}\n"
                               f"🔖 Commit: {commit_short}\n"
                               f"🎉 Toàn bộ các Node đã được khởi động lại thành công với mã nguồn mới nhất!")
                        send_telegram_message(msg)
                        print(f"[{datetime.datetime.now()}] Đang khởi động lại các monitors ngầm...")
                        subprocess.Popen(["./start_monitors.sh"], cwd=os.path.dirname(os.path.abspath(__file__)))
                        
            except Exception as loop_err:
                print(f"[{datetime.datetime.now()}] ⚠️ Lỗi trong vòng lặp chính của Monitor (Đã bắt lỗi để tránh crash): {loop_err}")
                time.sleep(5)
                
    except KeyboardInterrupt:
        print(f"\n[{datetime.datetime.now()}] Đang dừng chương trình theo dõi Auto-Update...")
        print("Đã tắt hoàn toàn.")

if __name__ == "__main__":
    main()

