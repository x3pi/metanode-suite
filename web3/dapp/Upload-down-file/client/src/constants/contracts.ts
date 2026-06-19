import type { Abi } from "viem";
import FileAbi from "../abi/File.json";

export interface ContractConfig {
  abi: Abi;
  address: `0x${string}`;
}
// export const privateKey = "be21792cee9dd376447c9de7fb3b4f49d58dfdd13f7e9d0886bfbb303e8db901"

export const privateKey = "fb64857fe95b55dff91a11d2da0c8db2dddb29f617d3d1ddaa9a9880733d5407"

export const contracts = {
  File: {
    abi: FileAbi as Abi,
    address: "0x087cdab97d38a3bfFcDee170739E8C11Af651569" as `0x${string}`,
  },
  // Thêm các contracts khác ở đây nếu cần
  // VíDụ:
  // TokenContract: {
  //   abi: TokenAbi as Abi,
  //   address: "0x..." as `0x${string}`,
  // },
} as const satisfies Record<string, ContractConfig>;

export type ContractName = keyof typeof contracts;
