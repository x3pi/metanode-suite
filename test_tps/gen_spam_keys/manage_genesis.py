#!/usr/bin/env python3
import json
import sys
import os

def load_json(filepath):
    with open(filepath, 'r') as f:
        return json.load(f)

def save_json(data, filepath):
    with open(filepath, 'w') as f:
        json.dump(data, f, indent=2)

def add_keys(genesis_file, keys_file):
    print(f"Đang đọc dữ liệu từ {genesis_file} và {keys_file}...")
    genesis = load_json(genesis_file)
    keys = load_json(keys_file)

    if "alloc" not in genesis:
        genesis["alloc"] = []

    existing_addresses = {acc.get("address", "").lower() for acc in genesis["alloc"]}
    added = 0

    print("Đang xử lý thêm accounts...")
    for k in keys:
        addr = k.get("address", "")
        if addr.lower() not in existing_addresses:
            genesis["alloc"].append({
                "address": addr,
                "balance": "2000000000000000000000000000000",
                "pending_balance": "0",
                "last_hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
                "device_key": "0x0000000000000000000000000000000000000000000000000000000000000000",
                "publicKeyBls": ""
            })
            existing_addresses.add(addr.lower())
            added += 1

    print(f"Đang lưu lại {genesis_file}...")
    save_json(genesis, genesis_file)
    print(f"✅ Đã THÊM {added} accounts mới vào {genesis_file}")

def remove_keys(genesis_file, keys_file):
    print(f"Đang đọc dữ liệu từ {genesis_file} và {keys_file}...")
    genesis = load_json(genesis_file)
    keys = load_json(keys_file)

    if "alloc" not in genesis:
        print("Không có accounts nào trong genesis để xóa.")
        return

    keys_to_remove = {k.get("address", "").lower() for k in keys}
    
    original_count = len(genesis["alloc"])
    print("Đang lọc và xóa accounts...")
    
    genesis["alloc"] = [
        acc for acc in genesis["alloc"] 
        if acc.get("address", "").lower() not in keys_to_remove
    ]
    
    removed = original_count - len(genesis["alloc"])

    print(f"Đang lưu lại {genesis_file}...")
    save_json(genesis, genesis_file)
    print(f"✅ Đã XÓA {removed} accounts khỏi {genesis_file}")

if __name__ == "__main__":
    if len(sys.argv) < 4:
        print("Sử dụng:")
        print("  Thêm: python3 manage_genesis.py add <genesis.json> <generated_keys.json>")
        print("  Xóa:  python3 manage_genesis.py remove <genesis.json> <generated_keys.json>")
        sys.exit(1)

    action = sys.argv[1].lower()
    genesis_path = sys.argv[2]
    keys_path = sys.argv[3]

    if not os.path.exists(genesis_path):
        print(f"Lỗi: Không tìm thấy file {genesis_path}")
        sys.exit(1)
    if not os.path.exists(keys_path):
        print(f"Lỗi: Không tìm thấy file {keys_path}")
        sys.exit(1)

    if action == "add":
        add_keys(genesis_path, keys_path)
    elif action == "remove":
        remove_keys(genesis_path, keys_path)
    else:
        print("Lệnh không hợp lệ. Chỉ hỗ trợ 'add' hoặc 'remove'")
