import os
import threading
import subprocess
import time
import json
import urllib.request
import datetime

def node_health_monitor():
    rpc_json_path = "/tmp/rpc_nodes.json"
    dead_nodes = {}
    print(f"[{datetime.datetime.now()}] 🩺 Khởi động Health Monitor Thread...")
    while True:
        try:
            if os.path.exists(rpc_json_path):
                with open(rpc_json_path, 'r') as f:
                    data = json.load(f)
                for key, url in data.get('nodes', {}).items():
                    try:
                        req = urllib.request.Request(url)
                        with urllib.request.urlopen(req, timeout=2) as resp:
                            if dead_nodes.get(key):
                                # Recovered
                                dead_nodes[key] = False
                                from ci_monitor_start_nodes import send_telegram_message, get_server_ip_info
                                send_telegram_message(f"🟢 [Health Monitor] Node {key} ({url}) đã hoạt động lại!")
                    except Exception as e:
                        if not dead_nodes.get(key):
                            dead_nodes[key] = True
                            from ci_monitor_start_nodes import send_telegram_message, get_server_ip_info
                            send_telegram_message(f"🚨🚨🚨 [Health Monitor] PHÁT HIỆN NODE CHẾT!\nNode: {key}\nURL: {url}\nLỗi: {e}")
        except Exception as e:
            pass
        time.sleep(10)

def block_hash_monitor():
    print(f"[{datetime.datetime.now()}] 🔗 Khởi động Block Hash Monitor Thread...")
    checker_dir = "/home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker"
    while True:
        try:
            subprocess.run(
                ["go", "run", "main.go", "--watch", "--interval", "5s", "--config", "config-m-nodes.json", "--no-stop-flag"],
                cwd=checker_dir
            )
        except Exception as e:
            time.sleep(5)

