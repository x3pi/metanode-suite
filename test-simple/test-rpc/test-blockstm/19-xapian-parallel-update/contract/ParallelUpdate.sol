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

contract ParallelUpdate {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string constant DB_NAME = "blockstm_parallel_xapian";
    
    // Each user gets their own document ID in Xapian
    mapping(address => uint256) public userDocIds;

    event ParallelUpdated(address indexed wallet, uint256 newCounter, uint256 docId);

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    function initializeDoc() external {
        require(userDocIds[msg.sender] == 0, "Already initialized");
        userDocIds[msg.sender] = fullDB.newDocument(DB_NAME, abi.encode(uint256(0)));
    }

    function incrementUser() external {
        uint256 docId = userDocIds[msg.sender];
        uint256 currentCount = 0;

        if (docId == 0) {
            // Auto-initialize if not exists
            docId = fullDB.newDocument(DB_NAME, abi.encode(uint256(1)));
            userDocIds[msg.sender] = docId;
            currentCount = 1;
        } else {
            // Read user's own document
            bytes memory data = fullDB.getDataDocument(DB_NAME, docId);
            currentCount = abi.decode(data, (uint256));
            
            // Increment
            currentCount += 1;
            
            // Write back to user's own document, updating docId
            userDocIds[msg.sender] = fullDB.setDataDocument(DB_NAME, docId, abi.encode(currentCount));
        }
        
        emit ParallelUpdated(msg.sender, currentCount, userDocIds[msg.sender]);
    }

    function getUserDataFromDB(address user) external returns (uint256) {
        uint256 docId = userDocIds[user];
        require(docId != 0, "Not initialized");
        
        bytes memory data = fullDB.getDataDocument(DB_NAME, docId);
        require(data.length > 0, "Data not found in Xapian");
        return abi.decode(data, (uint256));
    }
}
