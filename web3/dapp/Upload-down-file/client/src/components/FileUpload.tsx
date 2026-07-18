import { useState, useCallback } from "react";
import { uploadFile, type UploadProgress } from "../utils/fileUpload";
import { downloadFileAndSave } from "../utils/fileDownload";
import { useEffect } from "react";
import { createPublicClient, http } from "viem";
import { chain991 } from "../constants/customChain";
import { contracts } from "../constants/contracts";
// Định nghĩa bên ngoài để tránh re-render
const MAX_FILE_SIZE = 2 * 1024 * 1024 * 1024;

const publicClient = createPublicClient({
  chain: chain991,
  transport: http(),
});

const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
};
export default function FileUpload() {
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState<UploadProgress | null>(null);
  const [isUploading, setIsUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isFileActive, setIsFileActive] = useState(false);
  const [elapsedTime, setElapsedTime] = useState<number | null>(null);

  // Lưu danh sách các fileKey đã active trong suốt phiên chạy của web
  const [activeKeys, setActiveKeys] = useState<Set<string>>(new Set());

  // Lắng nghe TỪ ĐẦU (khi component mới mount lên)
  useEffect(() => {
    const unwatch = publicClient.watchContractEvent({
      address: contracts.File.address as `0x${string}`,
      abi: contracts.File.abi as any,
      eventName: "FileActivated",
      onLogs: (logs) => {
        for (const log of logs) {
          const { fileKey: activatedKey } = (log as any).args;
          if (activatedKey) {
            console.log("🎉 Bắt được event FileActivated từ Blockchain:", activatedKey);
            setActiveKeys((prev) => new Set(prev).add(activatedKey.toLowerCase()));
          }
        }
      },
    });

    return () => unwatch();
  }, []);

  // Kiểm tra xem file hiện tại đã nằm trong danh sách activeKeys chưa
  useEffect(() => {
    if (progress?.stage === "completed" && progress.fileKey) {
      const fileKey = progress.fileKey.toLowerCase();
      if (activeKeys.has(fileKey)) {
        setIsFileActive(true);
      }
    }
  }, [progress?.stage, progress?.fileKey, activeKeys]);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selectedFile = e.target.files?.[0];
    if (selectedFile) {
      if (selectedFile.size > MAX_FILE_SIZE) {
        setError("File quá lớn. Giới hạn tối đa là 2GB.");
        setFile(null);
        e.target.value = ""; // Clear input
        return;
      }
      setFile(selectedFile);
      setProgress(null);
      setIsFileActive(false);
      setElapsedTime(null);
    }
  }, []);

  const handleUpload = useCallback(async () => {
    if (!file) return;

    setIsUploading(true);
    setProgress({ stage: "preparing" });
    setIsFileActive(false);
    setElapsedTime(null);

    const startTime = Date.now();
    try {
      const fileKey = await uploadFile(file, (prog) => {
        setProgress(prog);
      });
      console.log("Upload completed! FileKey:", fileKey);
      const endTime = Date.now();
      setElapsedTime((endTime - startTime) / 1000);
    } catch (error: any) {
      console.error("Upload failed:", error);
      setProgress({ stage: "error", error: error.message });
    } finally {
      setIsUploading(false);
    }
  }, [file]);

  const getProgressPercentage = () => {
    if (!progress || !progress.totalChunks || progress.currentChunk === undefined) {
      return 0;
    }
    return Math.round((progress.currentChunk / progress.totalChunks) * 100);
  };

  const getStageText = () => {
    switch (progress?.stage) {
      case "preparing":
        return "Đang chuẩn bị file...";
      case "pushing":
        return "Đang gửi thông tin file lên blockchain...";
      case "uploading":
        return `Đang upload chunks: ${progress.currentChunk || 0}/${progress.totalChunks || 0}`;
      case "completed":
        return elapsedTime ? `Upload hoàn tất trong ${elapsedTime.toFixed(2)} giây!` : "Upload hoàn tất!";
      case "error":
        return `Lỗi: ${progress.error || "Unknown error"}`;
      default:
        return "Sẵn sàng";
    }
  };

  return (
    <div className="max-w-2xl mx-auto p-6 bg-white rounded-lg shadow-lg">
      <h2 className="text-2xl font-bold mb-6 text-gray-800">Upload File</h2>

      {/* File Input */}
      <div className="mb-6">
        <label className="block text-sm font-medium text-gray-700 mb-2">
          Chọn file để upload
        </label>
        <input
          type="file"
          onChange={handleFileChange}
          disabled={isUploading}
          className="block w-full text-sm text-gray-500
            file:mr-4 file:py-2 file:px-4
            file:rounded-full file:border-0
            file:text-sm file:font-semibold
            file:bg-blue-50 file:text-blue-700
            hover:file:bg-blue-100
            disabled:opacity-50 disabled:cursor-not-allowed"
        />
        {file && (
          <div className="mt-2 text-sm text-gray-600">
            <p><strong>Tên file:</strong> {file.name}</p>
            <p><strong>Kích thước:</strong> {formatFileSize(file.size)}</p>
          </div>
        )}
      </div>

      {/* Upload Button */}
      <button
        onClick={handleUpload}
        disabled={!file || isUploading}
        className="w-full bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed
          text-white font-semibold py-3 px-6 rounded-lg transition-colors duration-200"
      >
        {isUploading ? "Đang upload..." : "Upload File"}
      </button>

      {/* Progress Section */}
      {progress && (
        <div className="mt-6">
          <div className="flex items-center justify-between mb-2">
            <span className="text-sm font-medium text-gray-700">{getStageText()}</span>
            {progress.totalChunks && progress.currentChunk !== undefined && (
              <span className="text-sm text-gray-500">
                {getProgressPercentage()}%
              </span>
            )}
          </div>

          {/* Progress Bar */}
          {progress.totalChunks && progress.currentChunk !== undefined && (
            <div className="w-full bg-gray-200 rounded-full h-2.5">
              <div
                className="bg-blue-600 h-2.5 rounded-full transition-all duration-300"
                style={{ width: `${getProgressPercentage()}%` }}
              />
            </div>
          )}

          {/* FileKey Display */}
          {progress.fileKey && (
            <div className={`mt-4 p-3 border rounded-lg ${isFileActive ? "bg-green-50 border-green-200" : "bg-yellow-50 border-yellow-200"}`}>
              <div className="flex items-center justify-between mb-2">
                <p className={`text-sm font-medium ${isFileActive ? "text-green-800" : "text-yellow-800"}`}>
                  {isFileActive ? "🎉 File đã Active (Có thể tải về)" : "⏳ Đang chờ blockchain xác nhận..."}
                </p>
                {isFileActive && (
                  <button
                    onClick={() => navigator.clipboard.writeText(progress.fileKey!)}
                    className="text-xs bg-green-200 hover:bg-green-300 text-green-800 px-2 py-1 rounded"
                  >
                    Copy Key
                  </button>
                )}
              </div>
              <p className={`text-xs font-mono break-all ${isFileActive ? "text-green-700" : "text-yellow-700"}`}>
                {progress.fileKey}
              </p>
            </div>
          )}

          {/* Error Display */}
          {progress.stage === "error" && (
            <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-lg">
              <p className="text-sm font-medium text-red-800">Lỗi:</p>
              <p className="text-xs text-red-700">{progress.error}</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

