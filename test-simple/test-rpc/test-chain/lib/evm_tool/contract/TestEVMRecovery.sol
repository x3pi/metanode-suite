// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @title TestEVMRecovery - Contract xác thực tính toàn vẹn trạng thái EVM qua Restart & Snapshot Recovery
contract TestEVMRecovery {
    struct RoundRecord {
        uint256 round;
        string key;
        string value;
        uint256 count;
        uint256 timestamp;
    }

    uint256 public counter;
    string public lastKey;
    string public lastValue;
    uint256 public lastRound;

    mapping(string => string) private stateMap;
    mapping(uint256 => RoundRecord) private roundRecords;

    event StateUpdated(
        uint256 indexed round,
        string key,
        string value,
        uint256 indexed counter,
        address sender
    );

    /// @notice Khởi tạo / cập nhật trạng thái theo vòng lặp test
    function setRoundState(uint256 round, string memory key, string memory value) external {
        counter += 1;
        stateMap[key] = value;
        lastKey = key;
        lastValue = value;
        lastRound = round;

        roundRecords[round] = RoundRecord({
            round: round,
            key: key,
            value: value,
            count: counter,
            timestamp: block.timestamp
        });

        emit StateUpdated(round, key, value, counter, msg.sender);
    }

    /// @notice Cập nhật một key bất kỳ và tăng counter
    function updateState(string memory key, string memory value) external {
        counter += 1;
        stateMap[key] = value;
        lastKey = key;
        lastValue = value;

        emit StateUpdated(lastRound, key, value, counter, msg.sender);
    }

    /// @notice Đọc giá trị theo key từ mapping
    function getState(string memory key) external view returns (string memory) {
        return stateMap[key];
    }

    /// @notice Đọc bản ghi lịch sử của một vòng
    function getRoundRecord(uint256 round) external view returns (
        uint256 r,
        string memory key,
        string memory value,
        uint256 count,
        uint256 timestamp
    ) {
        RoundRecord memory rec = roundRecords[round];
        return (rec.round, rec.key, rec.value, rec.count, rec.timestamp);
    }

    /// @notice Đọc tóm tắt trạng thái hiện tại của contract
    function getSummary() external view returns (
        uint256 currentCounter,
        uint256 currentRound,
        string memory currentKey,
        string memory currentValue
    ) {
        return (counter, lastRound, lastKey, lastValue);
    }
}
