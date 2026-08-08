// src/customChain.ts
import { type Chain } from "viem";

// Bật cờ này thành `true` khi deploy lên server thật có SSL và tên miền chuẩn
export const IS_PRODUCTION = true;
// export const GO_BACKEND_RPC_URL = window.location.origin;
// export const WS_BASE = window.location.origin.replace(/^http/, "ws");
// export const WSS_RPC = `${WS_BASE}/interceptor`;

// export const WSS_RPC = "wss://192.168.1.233:8446";
// export const GO_BACKEND_RPC_URL = "https://192.168.1.233:8446";
// export const WSS_RPC = "ws://192.168.1.233:8545";
// export const GO_BACKEND_RPC_URL = "http://192.168.1.233:8545";

// export const WSS_RPC = "ws://139.59.243.85::8545";
// export const GO_BACKEND_RPC_URL = "http://139.59.243.85:8545";

export const GO_BACKEND_RPC_URL = "https://rpc-proxy-sequoia.iqnb.com:8446";
export const WSS_RPC = "wss://rpc-proxy-sequoia.iqnb.com:8446";

// Cấu hình các server tải file
// export const DOWNLOAD_SERVER_1 = "https://192.168.1.233:8081";
// export const DOWNLOAD_SERVER_2 = "https://192.168.1.233:8082";
export const DOWNLOAD_SERVER_1 = "https://file-keeper-2.iqnb.com:8081";
export const DOWNLOAD_SERVER_2 = "https://file-keeper-1.iqnb.com:8082";

// Replace with your actual Chain ID 991 details
export const chain991 = {
  id: 991,
  name: "My Chain 991", // Give your network a descriptive name
  nativeCurrency: {
    name: "My Native Token",
    symbol: "MNT",
    decimals: 18,
  },
  rpcUrls: {
    default: { http: [GO_BACKEND_RPC_URL] },
    public: { http: [GO_BACKEND_RPC_URL] },
  },
  // Optional: Add block explorer if you have one
  // blockExplorers: {
  //   default: { name: 'MyExplorer', url: 'http://localhost:4000' },
  // },
} as const satisfies Chain;
