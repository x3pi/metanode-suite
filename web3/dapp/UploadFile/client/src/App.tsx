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
  // Không cần lắng nghe ở đây nữa vì FileUpload.tsx đã tự xử lý

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
