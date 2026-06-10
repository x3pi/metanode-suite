import { useEffect } from "react";
import { createPublicClient, http, type Abi, type Hex } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { chain991 } from "./constants/customChain";
import { contracts, privateKey } from "./constants/contracts";
import FileUpload from "./components/FileUpload";
import WtTestPanel from "./components/WtTestPanel";
import { downloadFileAndSave } from "./utils/fileDownload";

const publicClient = createPublicClient({
  chain: chain991,
  transport: http(),
});

const account = privateKeyToAccount(`0x${privateKey}` as Hex);

function App() {
  // Lắng nghe sự kiện FileActivated từ smart contract
  useEffect(() => {
    const unwatch = publicClient.watchContractEvent({
      address: contracts.File.address as `0x${string}`,
      abi: contracts.File.abi as Abi,
      eventName: "FileActivated",
      onLogs: (logs) => {
        for (const log of logs) {
          const { user, fileKey } = (log as any).args;
          if (user?.toLowerCase() === account.address.toLowerCase()) {
            console.log("🎉 FileActivated:", fileKey);
            downloadFileAndSave(fileKey, (msg) => console.log(`[Download]: ${msg}`));
          }
        }
      },
    });
    return () => unwatch();
  }, []);

  return (
    <div className="min-h-screen bg-gray-100 py-8">
      <div className="container mx-auto space-y-6">
        <FileUpload />
        <WtTestPanel />
      </div>
    </div>
  );
}

export default App;
