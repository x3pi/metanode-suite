# fix bug

khi gọi start_unified_epoch_monitor thì cần hủy tiên trình ngầm đó để tránh bị lỗi

```bash
# log đồng thuận rust : check bao nhiêu stake
info!("Consensus committee: {:?}", committee);

🔄 [EPOCH RECOVERY] Extracting  : node clean epoch pending
🗑️ [DEBUG-RÁC]  : log check dọn rác

Để kiểm tra trực tiếp xem có bao nhiêu dữ liệu đang "lơ lửng" trên RAM mà chưa kịp lưu xuống ổ cứng, bạn có thể chạy lệnh này trên Terminal của hệ điều hành Linux (tại máy Node 4) trong lúc Node đang hoạt động:
cat /proc/meminfo | grep Dirty


# xem log
grep -rn "\[PERF-POOL-BATCH\] addTransactionsToPoolInternal took" /home/abc/nhat/con-chain-v2/metanode/consensus/logs_systemd/run_20260618_080945/
grep -rnE "\[BLOCK-TRACE\]|\[PERF-EVM\]" /home/abc/nhat/con-chain-v2/metanode/consensus/logs_systemd/run_20260618_080945/

```

key genesis:
 {
    "index": 0,
    "private_key": "31d9b4391503b818fcc0272eaa3f2e9c517fd1851c9e8818b17c6d4e6a0acba8",
    "address": "0x8cF200967660DB21739CaC872e64Bb5cfFBA0049"
  },
  {
    "index": 1,
    "private_key": "f2a45ec7c59aea49ce421aacf231ed98bf9d3092f36b58306be949457b651409",
    "address": "0x7174Ad7F17a5B57a7d1835ba1a942521407c0dC6"
  },
  
# sủa env deploy ssh nhiều máy
sửa :  
/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history/config-mutil.json
/home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json
/home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker/config-m-nodes.json
# đang còn lỗi validator -> synOnly



🚨 [EXECUTION-STALL] Go execution stuck at


🚨 [LIVENESS-STALL] DAG commit frozen at

### 18 Wallet Pool Keys (Funded in Genesis)
- Wallet 1: Address: `0x4b51d69B903C136654D168d0d500dA58AFdc5b60`, Private Key: `3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d`
- Wallet 2: Address: `0xd5D1c7e1c276288Fa0993bB7B1cF40C73f1226A4`, Private Key: `2aad2565bed5347214de1c14752933e9a410a76daed530e80ed6ce7af9363cf4`
- Wallet 3: Address: `0xB5814D58e8Bd09673fA41b41F53846334970e7Ed`, Private Key: `1b62dc8e370379da49eacebe54bafe6a1abf22a9cd89a0c461f41ee26a4d0722`
- Wallet 4: Address: `0x40123c5B48C14A3945B73A91429705028c83DcFc`, Private Key: `7c0ea8311b5aea187f3d716e67358f4d0b27b2a92d37d9582a708e6d2f51b8aa`
- Wallet 5: Address: `0x2f45Bc898316E8Aa9F1Bb530A710a7840D50Aa0b`, Private Key: `0e81c71ec598dfef0d28604a38368c47ee433468db567065cf5c38f4fafd337a`
- Wallet 6: Address: `0xFA6c3CB0785F29905528bB42172ae03aF642Ba54`, Private Key: `afbfdbca0cc62de736f85bc00e8a54003f87ae510df6aa396e9e5e69fbf35be8`
- Wallet 7: Address: `0x02bF9dB2b056c5816cCE1eeCea5F89748c71fCdD`, Private Key: `6bcf761ff5912cbc65653a8b0eec658213404eb88caedcdf42118a62edb663b8`
- Wallet 8: Address: `0x5Ba5c3C959e1100CB7B6B483048c22a1D33b10E8`, Private Key: `70d12ba6bbf9932f5d438b934141c43dcbb2171cd73a1c43571e8771e64574fa`
- Wallet 9: Address: `0xE13240be6f35C257d91A9f676c8C03501f9bf01d`, Private Key: `741cbc3e06ec8ea58513e2643c6935ac22ec1cb604307e9078a08160e4285fb3`
- Wallet 10: Address: `0x4bd11A56F174facBFe784C9c43E255622D2a02B1`, Private Key: `44dd469934018aa38d377571595c94009ff1877173c2e28ba1abb784bfcfc6a0` -- use
- Wallet 11: Address: `0x248BF9E035E4C3da95FECC94A0bF9A1e1F648a46`, Private Key: `d3d8157f2571153bcb664233f998a82b9b475fe509f92caf65ca2461bae7f1a9`
- Wallet 12: Address: `0xF925262a405194Db7fbDFF02f111cDfaa3F8E54F`, Private Key: `f5e6ba1cb14367c5264317dcb5f6e13f0d3cb0e3618e0a91f768570ab94b489c`
- Wallet 13: Address: `0xeb9fB85817bdC05fC1fAA43984500847e2006884`, Private Key: `fc1ee6ee9341cbc12a7b214ba3a70955821fb6ae568a3bde8beb5681d782b713`
- Wallet 14: Address: `0x4eCB82724890c656D09761b617369479c5392d6D`, Private Key: `cea98b91856e9f514f9fc9c0f614f0c4297915cc39d298de6f32ad77d0918076`
- Wallet 15: Address: `0x3b97a0aa19f157E7B2BeD22f43d2B187d89ca135`, Private Key: `826ebf3001a807518384ab4d2fd1174d01ad9d216c02c1107e482331f8da3035`
- Wallet 16: Address: `0xDeEBF59cb5fddd8926C6E8f6A131bd25E60C9fb1`, Private Key: `fe9f196740e1e0133be75309f22f176b890e655e1d6aa9cc4c271098dc3681ef`
- Wallet 17: Address: `0xdC6640eAb14767854C2793e599945cd7F2E9AB10`, Private Key: `a056f4776044200dba1efc895e5b3f6073100a864ed8a950f332470c7abdcc3b`
- Wallet 18: Address: `0xFAdc1883451766B5274671032B374b82fbF12019`, Private Key: `a8f0af85e301ac47ab2a20f33161c3fa4ac9d22d0ecc9bd035900c85e0a3bd3c`


{
  "101": [
    {
      "private_key": "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106",
      "address": "0xF6266Bbc0c65235E6A9eBA3f9e710862e2179E33",
      "role": "Sender (A0)"
    },
    {
      "private_key": "edb63ea1c26ce5c5d29df010ddedf2c57b3a4af38d776290f9a789205366acea",
      "address": "0xE3784d5212FB306198d1f84231428B5ab2878f04",
      "role": "Dev A1"
    },
    {
      "private_key": "7e6b1b687e6ac70d42ae2d4ab007bc3d9d78c24fb0af5affc601bc4abb25549b",
      "address": "0xA6E3C7A286F65862c37F9AD794B498ab13b8d3Fc",
      "role": "Dev A2"
    },
    {
      "private_key": "37f2b66e37b3511e7510177457e959623da3f6e0e219d1905314d2da1cc06f63",
      "address": "0x0d038819f8aDE9e92B0A24d65582223c5Bf42546",
      "role": "Dev A3"
    },
    {
      "private_key": "fdf24aade8d4ad95c03a0528dda1a973467503410dde8d90e58d42f26147e60e",
      "address": "0xE18d48129515D63D475a41093CaE23Ca004F8179",
      "role": "Dev A4"
    },
    {
      "private_key": "d2ba003dc705b5b802fa529e292ae83b91774f1c4c73c2a5ee822840179ab656",
      "address": "0x45d90D4c9c5861404AA5186DA621C75Acad7bF06",
      "role": "Relayer A (A5)"
    }
  ],
  "102": [
    {
      "private_key": "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a",
      "address": "0x93eAc269F0d2E15bC7B4CeE49f0f4185b585E340",
      "role": "Recipient (B0)"
    },
    {
      "private_key": "e9cd1c4831e3999edb9faf41a099e767754dcab285446b6d318f7e816788f506",
      "address": "0xd2452779F76B4EE7d8b0d1B39EE31FF8e34Aa330",
      "role": "Dev B1"
    },
    {
      "private_key": "973870d60e7ece6b391077fbeebcbd76d2486e1aaa0c3defe9bf5336574bffe2",
      "address": "0xa03A7760e464f131f4b3Eb064679fc34a01fb1f5",
      "role": "Dev B2"
    },
    {
      "private_key": "c72db8b139044753754f3cc98306053d069a0d0fd6b4ce8a1b9e69e4b11b703e",
      "address": "0x64A3D425F05d91002e3f25c16e77E7699622caA2",
      "role": "Dev B3"
    },
    {
      "private_key": "cc8b620a56e75c1694e3c11f1d94fd706ea3984bd4dd73010f5c70f59767f8bc",
      "address": "0x7886B5D60c3a559251506e073052f4135c657688",
      "role": "Dev B4"
    },
    {
      "private_key": "f3d4eb300bae3654b268156af2a8b2f62db146d368249cd30a860956128310ec",
      "address": "0xF0a6048c2F96843be0E273CCc297dfe296DDf805",
      "role": "Relayer B (B5)"
    }
  ]
}
