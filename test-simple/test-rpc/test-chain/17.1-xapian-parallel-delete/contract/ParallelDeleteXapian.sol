// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

struct PrefixEntry {
    string key;
    string value;
}

struct RangeFilter {
    uint slot;
    string startSerialised;
    string endSerialised;
}

struct SearchParams {
    string queries;
    PrefixEntry[] prefixMap;
    string[] stopWords;
    uint64 offset;
    uint64 limit;
    int64 sortByValueSlot;
    bool sortAscending;
    RangeFilter[] rangeFilters;
}

struct SearchResultCore {
    uint256 docid;
    uint256 rank;
    int256 percent;
    bytes data;
}

struct SearchResultsPageCore {
    uint256 total;
    SearchResultCore[] results;
}

interface IFullDBV1 {
    function getOrCreateDb(string memory name) external returns (bool);
    function newDocument(string memory dbname, bytes memory data) external returns (uint256);
    function getDataDocument(string memory dbname, uint256 docId) external returns (bytes memory);
    function indexTextForDocument(string memory dbname, uint256 docId, string memory text, uint8 weight, string memory prefix) external returns (uint256);
    function deleteDocument(string memory dbname, uint256 docId) external returns (bool);
    function querySearch(string memory dbname, SearchParams memory params) external returns (SearchResultsPageCore memory);
}

contract ParallelDeleteXapian {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string constant DB_NAME = "blockstm_delete_xapian";

    uint256[10] public docIds;

    event DocumentCreated(uint256 index, uint256 docId);
    event DocumentDeleted(address indexed wallet, uint256 index, uint256 docId);
    event SearchExecuted(uint256 totalFound);

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    // Tạo 10 documents mẫu ban đầu
    function initDocs() external {
        for (uint256 i = 0; i < 10; i++) {
            string memory text = string(abi.encodePacked("delete_target_doc_", uint2str(i)));
            uint256 docId = fullDB.newDocument(DB_NAME, bytes(text));
            fullDB.indexTextForDocument(DB_NAME, docId, "delete_target_doc", 1, "");
            docIds[i] = docId;
            emit DocumentCreated(i, docId);
        }
    }

    // Xóa 1 document theo index
    function deleteDoc(uint256 index) external {
        require(index < 10, "Index out of range");
        uint256 docId = docIds[index];
        require(docId > 0, "Doc not initialized");
        fullDB.deleteDocument(DB_NAME, docId);
        emit DocumentDeleted(msg.sender, index, docId);
    }

    // Đọc data của document theo index
    function getDocData(uint256 index) external returns (bytes memory) {
        require(index < 10, "Index out of range");
        uint256 docId = docIds[index];
        return fullDB.getDataDocument(DB_NAME, docId);
    }

    // Tìm kiếm các document còn lại
    function searchDocs(string memory query) external returns (uint256) {
        SearchParams memory params;
        params.queries = query;
        params.offset = 0;
        params.limit = 10;

        SearchResultsPageCore memory result = fullDB.querySearch(DB_NAME, params);
        emit SearchExecuted(result.total);
        return result.total;
    }

    function uint2str(uint256 _i) internal pure returns (string memory str) {
        if (_i == 0) return "0";
        uint256 j = _i;
        uint256 length;
        while (j != 0) {
            length++;
            j /= 10;
        }
        bytes memory bstr = new bytes(length);
        uint256 k = length;
        j = _i;
        while (j != 0) {
            bstr[--k] = bytes1(uint8(48 + j % 10));
            j /= 10;
        }
        str = string(bstr);
    }
}
