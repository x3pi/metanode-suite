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