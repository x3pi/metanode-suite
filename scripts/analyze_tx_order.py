#!/usr/bin/env python3
import os
import re
from collections import defaultdict

# Path to the logs directory
LOGS_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "../../mtn-consensus/metanode/logs"))

def parse_logs():
    if not os.path.exists(LOGS_DIR):
        return -1, f"Error: Logs directory not found at {LOGS_DIR}"

    # gei -> node -> { "type": RUST or SYNC, "txs": [ (pos, hash) ] }
    block_txs = defaultdict(lambda: defaultdict(list))
    
    rust_pattern = re.compile(r"\[RUST-TX-ORDER\] GEI=(\d+), pos\[(\d+)/\d+\]: hash=([a-f0-9\.]+)")
    sync_pattern = re.compile(r"\[SYNC-TX-ORDER\] pos\[(\d+)/\d+\]: hash=([a-f0-9\.]+)")
    sync_gei_pattern = re.compile(r"\[SYNC-TX-ORDER\] Block #\d+ \(GEI=(\d+)\), txs=\d+")

    for node_id in range(5):
        node_dir = os.path.join(LOGS_DIR, f"node_{node_id}/go-master/epoch_0")
        if not os.path.isdir(node_dir):
            continue
            
        log_files = [f for f in os.listdir(node_dir) if f.startswith("runSocketExecutor_") and f.endswith(".log")]
        if not log_files:
            continue
            
        log_file = max(log_files, key=lambda f: os.path.getmtime(os.path.join(node_dir, f)))
        log_path = os.path.join(node_dir, log_file)
        
        current_sync_gei = None
        
        with open(log_path, "r", errors="replace") as f:
            for line in f:
                match = rust_pattern.search(line)
                if match:
                    gei, pos, tx_hash = match.groups()
                    block_txs[gei][f"node{node_id}"].append(("RUST", int(pos), tx_hash))
                    continue
                
                match = sync_gei_pattern.search(line)
                if match:
                    current_sync_gei = match.group(1)
                    continue
                    
                if current_sync_gei:
                    match = sync_pattern.search(line)
                    if match:
                        pos, tx_hash = match.groups()
                        block_txs[current_sync_gei][f"node{node_id}"].append(("SYNC", int(pos), tx_hash))

    discrepancies = 0
    details = ""
    for gei in sorted(block_txs.keys(), key=int):
        nodes_data = block_txs[gei]
        if len(nodes_data) <= 1:
            continue
            
        reference_node = None
        reference_txs = None
        
        for node, tx_list in nodes_data.items():
            tx_list_sorted = sorted(tx_list, key=lambda x: x[1])
            tx_hashes = [tx[2] for tx in tx_list_sorted]
            
            if reference_node is None:
                reference_node = node
                reference_txs = tx_hashes
            else:
                if tx_hashes != reference_txs:
                    details += f"❌ LỆCH THỨ TỰ TX GEI={gei} giữa {reference_node} và {node}\n"
                    discrepancies += 1

    if discrepancies == 0:
        return 0, "✅ All nodes received transactions in the exact same order for all checked GEIs."
    else:
        return discrepancies, details

if __name__ == "__main__":
    count, msg = parse_logs()
    print(msg)
