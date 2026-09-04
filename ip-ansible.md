all:
  children:
    metanode_cluster:
      vars:
        telegram_bot_token: "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
        telegram_chat_id: "-1003867050625"
      hosts:
        192.168.1.233_validator:
          node_ids: [0]
          rpc_nodes: [0]
          ansible_connection: local
          ansible_host: 192.168.1.233
          ansible_user: "abc"
          ansible_ssh_pass: "1234@abcd"
          ansible_become_pass: "1234@abcd"
        192.168.1.232_validator:
          node_ids: [1,2]
          rpc_nodes: [1,2]
          ansible_host: 192.168.1.232
          ansible_user: "abc"
          ansible_ssh_pass: "1234@abcd"
          ansible_become_pass: "1234@abcd"




all:
  children:
    metanode_cluster:
      vars:
        telegram_bot_token: "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
        telegram_chat_id: "-1003867050625"
        ansible_user: "abc"
        ansible_ssh_pass: "1234@abcd"
        ansible_become_pass: "1234@abcd"
      hosts:
        192.168.1.232_server:
          node_ids: [1, 2, 4]
          synconly_nodes: [4]
          rpc_nodes: [1, 2, 4]
          ansible_host: 192.168.1.232
        192.168.1.233_server:
          node_ids: [0, 3]
          rpc_nodes: [0, 3]
          ansible_connection: local
          ansible_host: 192.168.1.233






          all:
  children:
    metanode_cluster:
      vars:
        telegram_bot_token: "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
        telegram_chat_id: "-1003867050625"
        epoch_duration_seconds: 600
        snapshot_frequency_blocks: 500
        snapshot_max_snapshots: 2
        snapshot_max_part_size_mb: 600
        epochs_to_keep: 5  # Mặc định: 0 = Không xóa DB
      hosts:
        192.168.1.234_validator:
          node_ids: [0]
          rpc_nodes: [0]
          prune_nodes: [0]  # 👈 Muốn xóa DB node nào thì điền vào đây, ví dụ: [0]
          ansible_connection: local
          ansible_host: 192.168.1.234
          ansible_user: "abc"
          ansible_ssh_pass: "1234@abcd"
          ansible_become_pass: "1234@abcd"
        192.168.1.231_validator:
          node_ids: [2]
          rpc_nodes: [2]
          prune_nodes: [2]  # 👈 Muốn xóa DB node nào thì điền vào đây, ví dụ: [0]
          ansible_host: 192.168.1.231
          ansible_user: "abc"
          ansible_ssh_pass: "1234@abcd"
          ansible_become_pass: "1234@abcd"
        192.168.1.230_validator:
          node_ids: [1]
          rpc_nodes: [1]
          prune_nodes: [1]  # 👈 Muốn xóa DB node nào thì điền vào đây, ví dụ: [0]
          ansible_host: 192.168.1.230
          ansible_user: "abc"
          ansible_ssh_pass: "1234@abcd"
          ansible_become_pass: "1234@abcd"