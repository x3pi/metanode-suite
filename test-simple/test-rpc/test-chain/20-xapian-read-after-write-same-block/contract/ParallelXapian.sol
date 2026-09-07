// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IFullDBV1 {
    function getOrCreateDb(string memory name) external returns(bool);
    function newDocument(string memory dbname, bytes memory data) external returns(uint256);
    function setDataDocument(string memory dbname, uint256 docId, bytes memory data) external returns(uint256);
    function getDataDocument(string memory dbname, uint256 docId) external returns(bytes memory);
}

contract ParallelXapian {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string constant DB_NAME = "blockstm_parallel_xapian_getdata";

    uint256 public sharedDocId;

    event DocumentAdded(uint256 docId);
    event DocumentUpdated(uint256 docId);
    event DocumentRead(uint256 docId, string data);

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    // Step 1: Chạy riêng biệt trước để tạo document lấy ID
    function setupDocument(string memory initialText) external {
        sharedDocId = fullDB.newDocument(DB_NAME, bytes(initialText));
        emit DocumentAdded(sharedDocId);
    }

    // Step 2: Lệnh GHI (Cập nhật document đã có)
    function updateDocument(string memory newText) external {
        require(sharedDocId > 0, "Must call setupDocument first");
        fullDB.setDataDocument(DB_NAME, sharedDocId, bytes(newText));
        emit DocumentUpdated(sharedDocId);
    }

    // Step 3: Lệnh ĐỌC (Lấy document theo ID)
    function readDocument() external {
        require(sharedDocId > 0, "Must call setupDocument first");
        bytes memory data = fullDB.getDataDocument(DB_NAME, sharedDocId);
        emit DocumentRead(sharedDocId, string(data));
    }
}
