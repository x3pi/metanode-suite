/**
 * WtTestPanel.tsx
 * Giao diện test thủ công tải 1 chunk qua WebTransport.
 * Dùng để debug và xác nhận server wt_server.rs hoạt động đúng.
 */
import { useRef, useState } from "react";
import { downloadFileAndSave } from "../utils/fileDownload";

export default function WtTestPanel() {
  const [fileKey, setFileKey] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);

  const addLog = (msg: string) => {
    const time = new Date().toLocaleTimeString();
    setLogs((prev) => {
      const next = [...prev, `[${time}] ${msg}`];
      // auto-scroll
      setTimeout(() => logEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);
      return next;
    });
  };

  const handleTest = async () => {
    const key = fileKey.trim();

    if (!key) {
      addLog("❌ Vui lòng điền File Key (hex, có 0x)");
      return;
    }

    setLoading(true);
    addLog(`🚀 Bắt đầu tải file: ${key}`);

    try {
      await downloadFileAndSave(key, (msg) => {
        addLog(msg);
      });
      addLog(`✅ Hoàn tất tải file và lưu xuống máy!`);
    } catch (err) {
      addLog(`❌ Exception: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="bg-white rounded-xl shadow-md p-6 max-w-3xl mx-auto">
      <h2 className="text-xl font-bold text-gray-800 mb-4">
        🧪 WebTransport Chunk Download Test
      </h2>

      <div className="grid grid-cols-2 gap-3 mb-4">
        {/* File Key */}
        <div className="col-span-2">
          <label className="block text-sm font-medium text-gray-600 mb-1">
            File Key <span className="text-gray-400 text-xs">(hex, có 0x)</span>
          </label>
          <input
            type="text"
            value={fileKey}
            onChange={(e) => setFileKey(e.target.value)}
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-indigo-400"
            placeholder="0xabc123..."
          />
        </div>

        {/* Submit */}
        <div className="col-span-2 flex items-end">
          <button
            onClick={handleTest}
            disabled={loading}
            className="w-full bg-indigo-600 hover:bg-indigo-700 disabled:bg-gray-400 text-white font-semibold py-2 px-4 rounded-lg transition"
          >
            {loading ? "⏳ Đang xử lý..." : "🚀 Tải File qua WebTransport"}
          </button>
        </div>
      </div>

      {/* Log console */}
      <div className="bg-gray-900 text-green-400 font-mono text-xs rounded-lg p-4 h-52 overflow-y-auto">
        {logs.length === 0 ? (
          <span className="text-gray-500">Logs sẽ hiện ở đây sau khi bấm Test...</span>
        ) : (
          logs.map((line, i) => <div key={i}>{line}</div>)
        )}
        <div ref={logEndRef} />
      </div>

      <button
        onClick={() => setLogs([])}
        className="mt-2 text-xs text-gray-400 hover:text-gray-600 transition"
      >
        🗑 Xóa log
      </button>
    </div>
  );
}
