// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

// Precompile Interface (V1) - 0x107
interface IFullDBV1 {
    function getOrCreateDb(string memory name) external returns (bool);
    function newDocument(
        string memory dbname,
        bytes memory data
    ) external returns (uint256);
    function getDataDocument(
        string memory dbname,
        uint256 docId
    ) external returns (bytes memory);
    function setDataDocument(
        string memory dbname,
        uint256 docId,
        bytes memory data
    ) external returns (uint256);
}

contract SharedUpdate {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string constant DB_NAME = "blockstm_shared_xapian";
    uint256 public sharedDocId;

    event SharedUpdated(address indexed wallet, uint256 newCounter, uint256 docId);

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    function initializeDoc() external {
        sharedDocId = fullDB.newDocument(DB_NAME, abi.encode(uint256(0)));
    }

    function incrementShared() external {
        // 2. LÀM THEO ĐÚNG Ý BẠN: Đọc biến count trực tiếp từ Xapian
        bytes memory data = fullDB.getDataDocument(DB_NAME, sharedDocId);
        uint256 currentCount = abi.decode(data, (uint256));
        
        // 3. Tăng lên 1
        currentCount += 1;
        
        // 4. Ghi ngược lại vào Xapian
        fullDB.setDataDocument(DB_NAME, sharedDocId, abi.encode(currentCount));
        
        emit SharedUpdated(msg.sender, currentCount, sharedDocId);
    }

    function getSharedDataFromDB() external returns (uint256) {
        bytes memory data = fullDB.getDataDocument(DB_NAME, sharedDocId);
        require(data.length > 0, "Data not found in Xapian");
        return abi.decode(data, (uint256));
    }
}
