# MetaNode Automation Test Pipeline (`auto_test.sh`)

This document provides a comprehensive guide on how to use the automated testing script `auto_test.sh` included in the `mtn-simple-2025` repository.

## Overview

The `auto_test.sh` script is an end-to-end automation pipeline designed to facilitate testing for the MetaNode blockchain ecosystem. It orchestrates the entire lifecycle of a test network:
1. Cleaning up and regenerating the genesis configuration.
2. Generating a massive set of spam keys for simulated loads.
3. Deploying the local MetaNode cluster (supporting both single-machine local testnets and multi-machine setups).
4. Running validation tests for basic TCP RPC functionality.
5. Testing advanced MVM capabilities including the Xapian V0 precompile.
6. Testing basic HTTP JSON-RPC capabilities for sending native currency transactions.
7. Testing advanced MVM capabilities including the Xapian V2 precompile.
15. Triggering a massive TPS (Transactions Per Second) load test via parallel native transfers.
16. Validating historical state querying by verifying account balances and nonces at older block heights (Test History RPC).

## Prerequisites

- You must execute this script from within a bash terminal.
- Ensure your `$PROJECT_ROOT` and paths (`/home/abc/nhat/consensus-chain/...`) are correct and accessible.
- Dependencies such as `go`, binary compilation toolchains, and necessary shell scripts (e.g., `mtn-orchestrator.sh` or `deploy_cluster.sh`) must be executable.

## Usage & Arguments

You can run the script normally to execute all steps sequentially from the beginning:

```bash
./auto_test.sh
```

### Supported Arguments

The script supports overriding the starting step and the deployment topology mode.

| Argument | Value Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--step` or `--steps` | `String` or `Integer` | `ALL` | Specifies exactly which steps to execute. You can provide a single step (e.g., `--step 3`) or a list of steps separated by commas or spaces (e.g., `--steps "2,4,5"`). If omitted, the script executes all steps sequentially. |
| `--mode` | `single` \| `multi`| `single`| The topology used in Step 2. `single` uses `mtn-orchestrator.sh` for a 1-machine cluster. `multi` uses `deploy_cluster.sh` with `deploy-3machines.env` for a multi-machine testing layout. |

### Examples

**1. Run a fresh multi-machine test from the beginning (all steps):**
```bash
./auto_test.sh --mode multi
```

**2. The cluster is already deployed. Run only the TPS load testing phase (Step 7):**
```bash
./auto_test.sh --step 7 --mode multi
```

**3. Run specific steps only (e.g. Cluster Deploy, Xapian V0, and Xapian V2):**
```bash
./auto_test.sh --steps "2,4,6" --mode multi
```

## Pipeline Steps Explained

### Bước 1: Prepare Genesis & Gen Spam Keys
- **Location:** `cmd/simple_chain` and `cmd/tool/test_tps/gen_spam_keys`
- **Action:** Refreshes `genesis.json` from `genesis-main.json` to ensure clean state and generates 50,000 unique key pairs (`generated_keys.json`) used subsequently by the load tester.

### Bước 2: Deploy Cluster
- **Location:** `mtn-consensus/metanode/scripts/...` or `metanode-suite/scripts/`
- **Action:** 
  - If `--mode single`: Runs `mtn-orchestrator.sh restart --fresh --build-all`.
  - If `--mode multi`: Runs `deploy_cluster.sh --env deploy-3machines.env --all`.
- **Note:** Includes a brief 5-second sleep to ensure HTTP/RPC servers settle before pushing queries.

### Bước 3: Test TCP RPC
- **Location:** `cmd/tool/tool-test-chain/test-tcp/caller-tcp`
- **Action:** Runs `main-no-none.go` to test legacy raw TCP transaction injection.

### Bước 4: Test HTTP RPC - Xapian V0
- **Location:** `cmd/tool/tool-test-chain/test-rpc`
- **Action:** Runs a targeted script implementing the initial tests (read/write data) aimed at validating Xapian V0's integration within the C++ MVM engine.

### Bước 5: Test Send Native Coin
- **Location:** `cmd/tool/tool-test-chain/test-rpc/send-native`
- **Action:** Tests the basic HTTP JSON-RPC capabilities for sending native currency transactions over the network.

### Bước 6: Test HTTP RPC - Xapian V2
- **Location:** `cmd/tool/tool-test-chain/test-rpc`
- **Action:** Validates Xapian V2 integration updates over the JSON-RPC interface.

### Bước 7: Load Test TPS (Load Balancer & Parallel Execution)
- **Location:** `cmd/tool/test_tps/tps_blast_cc`
- **Action:** Triggers the TPS load tester with a 20,000 TX spray across 5 rounds. Uses parallel native transfers (`--parallel_native=true`) and round-robin connection pools (`--load_balance=true`).
- **Recent Updates for Debugging:** The tool is now equipped to automatically detect `invalid nonce` errors thrown by lagging consensus nodes. If an invalid nonce occurs, it triggers a cross-check array (Mismatch/Divergence Check) scanning all RPC targets in the pool concurrently and cleanly identifies which node represents stale network state. You can inspect the divergence table in your terminal during this step.

### Bước 8: Test History RPC
- **Location:** `test-simple/test-rpc/test-history`
- **Action:** Validates the historical state querying mechanism. It creates a transaction, records the exact state (balance and nonce) at the resulting block (Block A), advances the chain to a newer block (Block B), and then strictly verifies that querying the historical state at Block A via `eth_getBalance` and `eth_getTransactionCount` matches the previously recorded snapshot exactly.

## 24/7 CI/CD Monitoring (`ci_monitor.py`)

To continuously monitor the GitHub repository and run the test pipeline automatically on every new commit, use the `ci_monitor.py` script.

### Features
1. **GitHub Polling (Network-Light)**: It uses `git ls-remote` every 10 seconds to check for new commits on the `origin/main` branch without downloading data unnecessarily.
2. **Auto-Pull & Clean Restart**: If a new commit is detected, it terminates the currently running `auto_test.sh` process (via Process Group Kill), runs `git pull origin main`, and starts a fresh pipeline.
3. **Log Management**: Each automated run generates an isolated log file in `scripts/auto_test_logs/` labeled with the commit hash and timestamp to prevent overwriting.
4. **Telegram Alerts**: If the `auto_test.sh` pipeline fails (exits with code > 0), the script immediately sends an alert containing the commit hash and exit code to the configured Telegram group.

### How to Run
It is highly recommended to run this script in the background using `nohup` or `tmux` so it persists after you close your SSH session:

```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/scripts
nohup ./ci_monitor.py > ci_monitor.log 2>&1 &
```

**Passing Arguments to `auto_test.sh`:**
Any arguments passed to `ci_monitor.py` are forwarded directly to `auto_test.sh`. For example, to run the monitor only on specific steps in single mode:
```bash
nohup ./ci_monitor.py --mode single --steps "2,4,5" > ci_monitor.log 2>&1 &
nohup ./ci_monitor.py --mode single > ci_monitor.log 2>&1 &
./ci_monitor.sh --type spam

./ci_monitor.sh --type spam --batch 500
./ci_monitor.sh --type spam --no-listen

./ci_monitor.sh --type recovery
./ci_monitor.sh --type snapshot
./ci_monitor.sh --type spam_xapian

./ci_monitor.sh --type snapshot --no-listen
./ci_monitor.sh --type recovery --no-listen

./ci_monitor.sh stop
```

**To stop the monitor:**
```bash
pkill -f ci_monitor.py
```

## Troubleshooting

- **"Lỗi ở Bước X" (Error at Step X):** Look at the lines directly above the error message to view the output of the Go program. Fix the compilation or logic error, and use `./auto_test.sh --step X` to resume from the failure point without resetting everything.
- **Node 3 Deadlocks / "Missing Receipt":** If Step 7 hangs or states timeout waiting for receipts, one of the MVM Go/C++ consensus instances (potentially Master Node 3) has deadlocked. Check the `logs/node_X/go-master-stdout.log` logs to identify where GEI (Global Exec Index) stalled.
- **"invalid nonce":** This is expected under massive load testing when fetching state from weakly-synchronised sub-nodes. Step 7's TPS tool will print out a table diagnosing which node yielded the stale nonce.
