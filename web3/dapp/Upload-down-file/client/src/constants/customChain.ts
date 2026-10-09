// src/customChain.ts
import { type Chain } from "viem";

// Bật cờ này thành `true` khi deploy lên server thật có SSL và tên miền chuẩn
export const IS_PRODUCTION = false;
// export const GO_BACKEND_RPC_URL = window.location.origin;
// export const WS_BASE = window.location.origin.replace(/^http/, "ws");
// export const WSS_RPC = `${WS_BASE}/interceptor`;

// export const WSS_RPC = "wss://192.168.1.233:8446";
// export const GO_BACKEND_RPC_URL = "https://192.168.1.233:8446";
export const WSS_RPC = "ws://192.168.1.234:8747/ws";
export const GO_BACKEND_RPC_URL = "http://192.168.1.234:8747";

// export const WSS_RPC = "ws://139.59.243.85::8545";
// export const GO_BACKEND_RPC_URL = "http://139.59.243.85:8545";

// export const GO_BACKEND_RPC_URL = "https://rpc-proxy-sequoia.iqnb.com:8446";
// export const WSS_RPC = "wss://rpc-proxy-sequoia.iqnb.com:8446";

// Cấu hình các server tải file
export const DOWNLOAD_SERVER_1 = "https://192.168.1.230:8081";
export const DOWNLOAD_SERVER_2 = "https://192.168.1.230:8082";
// export const DOWNLOAD_SERVER_1 = "https://file-keeper-2.iqnb.com:8081";
// export const DOWNLOAD_SERVER_2 = "https://file-keeper-1.iqnb.com:8082";

// WebTransport Self-signed Certificate Hash (dùng cho môi trường Local / Non-Production)
export const WT_SERVER_CERTIFICATE_HASH = new Uint8Array([
  0x1d, 0xf2, 0x55, 0x75, 0xdc, 0x53, 0x74, 0xda, 0x35, 0x51, 0xe9, 0x03,
  0xb2, 0x1c, 0xc3, 0x52, 0x7c, 0xec, 0x23, 0x68, 0x66, 0xe6, 0x95, 0x8f,
  0x7a, 0xd8, 0x8c, 0x78, 0x93, 0xd6, 0xeb, 0xbd
]);

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
