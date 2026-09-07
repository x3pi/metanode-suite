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
    function getOrCreateDb(string memory name) external returns(bool);
    function newDocument(string memory dbname, bytes memory data) external returns(uint256);
    function indexTextForDocument(string memory dbname, uint256 docId, string memory text, uint8 weight, string memory prefix) external returns(uint256);
    function querySearch(string memory dbname, SearchParams memory params) external returns(SearchResultsPageCore memory);
}

contract ParallelXapian {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string constant DB_NAME = "blockstm_parallel_xapian";

    event DocumentAdded(uint256 docId);
    event SearchExecuted(uint256 totalFound);

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    function addDocument(string memory text) external {
        uint256 docId = fullDB.newDocument(DB_NAME, bytes(text));
        fullDB.indexTextForDocument(DB_NAME, docId, text, 1, "");
        emit DocumentAdded(docId);
    }

    function searchDocument(string memory query) external {
        SearchParams memory params;
        params.queries = query;
        params.offset = 0;
        params.limit = 10;
        // The other arrays are implicitly empty

        SearchResultsPageCore memory result = fullDB.querySearch(DB_NAME, params);
        emit SearchExecuted(result.total);
    }
}
