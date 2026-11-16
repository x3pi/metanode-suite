// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

struct PrefixEntry {
    string key;
    string value;
}

struct RangeFilter {
    uint256 slot;
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

struct SearchResult {
    uint256 docid;
    uint256 rank;
    int256 percent;
    bytes data;
}

struct SearchResultsPage {
    uint256 total;
    SearchResult[] results;
}

interface IFullDBV1 {
    function getOrCreateDb(string memory name) external returns (bool);
    function newDocument(string memory dbname, bytes memory data) external returns (uint256);
    function getDataDocument(string memory dbname, uint256 docId) external returns (bytes memory);
    function setDataDocument(string memory dbname, uint256 docId, bytes memory data) external returns (uint256);
    function deleteDocument(string memory dbname, uint256 docId) external returns (bool);
    function addTermDocument(string memory dbname, uint256 docId, string memory term) external returns (uint256);
    function indexTextForDocument(
        string memory dbname,
        uint256 docId,
        string memory text,
        uint8 weight,
        string memory prefix
    ) external returns (uint256);
    function addValueDocument(
        string memory dbname,
        uint256 docId,
        uint256 slot,
        string memory data,
        bool isSerialise
    ) external returns (uint256);
    function getValueDocument(
        string memory dbname,
        uint256 docId,
        uint256 slot,
        bool isSerialise
    ) external returns (string memory);
    function getTermsDocument(string memory dbname, uint256 docId) external returns (string[] memory);
    function querySearch(
        string memory dbname,
        SearchParams memory params
    ) external view returns (SearchResultsPage memory);
}

contract XapianMultiNodeSync {
    IFullDBV1 constant fullDB = IFullDBV1(0x0000000000000000000000000000000000000107);
    string public constant DB_NAME = "multinode_raft_sync_v1";

    uint256[3] public baselineDocIds;
    uint256 public counterDocId;
    uint256[] public parallelDocIds;
    uint256 public evmCounter;
    bytes32 public anchoredSearchDigest;

    constructor() {
        fullDB.getOrCreateDb(DB_NAME);
    }

    function seedBaseline() external {
        uint256 d0 = fullDB.newDocument(DB_NAME, bytes("baseline-alpha|v1"));
        fullDB.addTermDocument(DB_NAME, d0, "Sbaseline");
        fullDB.addValueDocument(DB_NAME, d0, 0, "100", true);
        fullDB.indexTextForDocument(DB_NAME, d0, "metanode consensus alpha", 1, "S");

        uint256 d1 = fullDB.newDocument(DB_NAME, bytes("baseline-bravo-phys2-guard|v1"));
        fullDB.addTermDocument(DB_NAME, d1, "Sbaseline");
        fullDB.addValueDocument(DB_NAME, d1, 0, "200", true);
        fullDB.indexTextForDocument(DB_NAME, d1, "metanode consensus bravo", 1, "S");

        uint256 d2 = fullDB.newDocument(DB_NAME, bytes("baseline-charlie-ephemeral|v1"));
        fullDB.addTermDocument(DB_NAME, d2, "Sbaseline");
        fullDB.addValueDocument(DB_NAME, d2, 0, "300", true);
        fullDB.indexTextForDocument(DB_NAME, d2, "metanode consensus charlie", 1, "S");

        baselineDocIds[0] = d0;
        baselineDocIds[1] = d1;
        baselineDocIds[2] = d2;

        counterDocId = fullDB.newDocument(DB_NAME, abi.encode(uint256(0)));
    }

    function writeShortHexDoc() external {
        uint256 shortId = 2;
        fullDB.setDataDocument(DB_NAME, shortId, bytes("short-docid-0x2|independent"));
        fullDB.addTermDocument(DB_NAME, shortId, "Sshortdoc");
        fullDB.addValueDocument(DB_NAME, shortId, 0, "250", true);
        fullDB.indexTextForDocument(DB_NAME, shortId, "metanode shortdoc guard", 1, "S");
    }

    function parallelCreateAndIndex(string calldata tag, string calldata scoreStr) external returns (uint256) {
        uint256 docId = fullDB.newDocument(DB_NAME, bytes(tag));
        fullDB.addTermDocument(DB_NAME, docId, "Sparallel");
        fullDB.addValueDocument(DB_NAME, docId, 0, scoreStr, true);
        // Identical token stream & weight across parallel txs so BM25 tie-breaks by physical Xapian::docid
        fullDB.indexTextForDocument(DB_NAME, docId, "metanode equalrank consensus", 1, "S");
        parallelDocIds.push(docId);
        return docId;
    }

    // Mutates Xapian (creates ghost doc, corrupts existing doc, deletes baseline[0]) and then REVERTS!
    // Verifies zero uncommitted Xapian buffer leakage on both Leader (Block-STM) and Followers (FullDbLogs).
    function revertAfterDirtyWrites(string calldata ghostTag) external {
        uint256 ghostId = fullDB.newDocument(DB_NAME, bytes(ghostTag));
        fullDB.addTermDocument(DB_NAME, ghostId, "Sghost");
        fullDB.indexTextForDocument(DB_NAME, ghostId, "metanode equalrank ghost", 1, "S");
        fullDB.setDataDocument(DB_NAME, baselineDocIds[1], bytes("CORRUPTED_BY_REVERTED_TX"));
        fullDB.deleteDocument(DB_NAME, baselineDocIds[0]);
        revert("intentional_xapian_revert");
    }

    function incrementSharedCounter() external {
        evmCounter += 1;
        bytes memory raw = fullDB.getDataDocument(DB_NAME, counterDocId);
        require(raw.length == 32, "counter doc missing");
        uint256 nextVal = abi.decode(raw, (uint256)) + 1;
        fullDB.setDataDocument(DB_NAME, counterDocId, abi.encode(nextVal));
    }

    function updateAndDeleteBaseline() external {
        uint256 d0 = baselineDocIds[0];
        uint256 d2 = baselineDocIds[2];
        fullDB.setDataDocument(DB_NAME, d0, bytes("baseline-alpha|v2-updated"));
        fullDB.addValueDocument(DB_NAME, d0, 0, "150", true);
        fullDB.indexTextForDocument(DB_NAME, d0, "metanode metanode boost", 2, "S");

        require(fullDB.deleteDocument(DB_NAME, d2), "delete d2 failed");
        // Deleting short numeric ID 1 (which was never created) must NOT delete physical docid 1
        fullDB.deleteDocument(DB_NAME, 1);
    }

    // Executes Xapian querySearch ON-CHAIN inside a block and anchors keccak256(abi.encode(page))
    // into EVM contract storage so any Xapian divergence directly alters NOMT/MPT StateRoot!
    function anchorSearchDigestOnChain(string calldata queryStr) external returns (bytes32) {
        SearchResultsPage memory page = _searchInternal(queryStr, -1, false, 50);
        bytes32 digest = keccak256(abi.encode(page));
        anchoredSearchDigest = digest;
        return digest;
    }

    function getTrackedIds()
        external
        view
        returns (
            uint256[3] memory baseIds,
            uint256 cntDocId,
            uint256 evmCnt,
            uint256[] memory parIds
        )
    {
        return (baselineDocIds, counterDocId, evmCounter, parallelDocIds);
    }

    function readDocSnapshot(uint256 docId)
        external
        returns (bytes memory data, string memory scoreVal, string[] memory terms)
    {
        data = fullDB.getDataDocument(DB_NAME, docId);
        if (data.length > 0) {
            scoreVal = fullDB.getValueDocument(DB_NAME, docId, 0, true);
            terms = fullDB.getTermsDocument(DB_NAME, docId);
        }
    }

    function readSharedCounter() external returns (uint256 xapianCnt, uint256 evmCnt) {
        bytes memory raw = fullDB.getDataDocument(DB_NAME, counterDocId);
        require(raw.length == 32, "counter doc missing");
        return (abi.decode(raw, (uint256)), evmCounter);
    }

    function _searchInternal(
        string calldata queryStr,
        int64 sortBySlot,
        bool sortAsc,
        uint64 limit
    ) internal view returns (SearchResultsPage memory) {
        PrefixEntry[] memory prefixes = new PrefixEntry[](1);
        prefixes[0] = PrefixEntry({key: "", value: "S"});
        string[] memory stopWords = new string[](0);
        RangeFilter[] memory ranges = new RangeFilter[](0);

        SearchParams memory params = SearchParams({
            queries: queryStr,
            prefixMap: prefixes,
            stopWords: stopWords,
            offset: 0,
            limit: limit,
            sortByValueSlot: sortBySlot,
            sortAscending: sortAsc,
            rangeFilters: ranges
        });
        return fullDB.querySearch(DB_NAME, params);
    }

    function queryCluster(
        string calldata queryStr,
        int64 sortBySlot,
        bool sortAsc,
        uint64 limit
    ) external view returns (SearchResultsPage memory) {
        return _searchInternal(queryStr, sortBySlot, sortAsc, limit);
    }
}
