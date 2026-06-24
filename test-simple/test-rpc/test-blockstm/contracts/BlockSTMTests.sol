// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// ---------------------------------------------------------
// Test 2: Read-Write Conflict
// ---------------------------------------------------------
contract ReadWriteConflict {
    uint256 public sharedData;
    mapping(address => uint256) public userReads;

    // Tx1 updates the shared data
    function writeData(uint256 val) external {
        sharedData = val;
    }

    // Tx2 reads the shared data and stores it in its own state
    // If Tx2 is executed before Tx1, but Block-STM correctly identifies the conflict,
    // Tx2 must be re-executed and get the NEW value of sharedData.
    function readDataAndSave() external {
        userReads[msg.sender] = sharedData;
    }
}

// ---------------------------------------------------------
// Test 3: High Contention (AMM Swap Simulation)
// ---------------------------------------------------------
contract AMMSimulator {
    uint256 public reserveA = 1000000 ether;
    uint256 public reserveB = 1000000 ether;
    
    event Swap(address indexed user, uint256 amountIn, uint256 amountOut);

    // Multiple users calling this simultaneously will create massive read/write conflicts
    // on reserveA and reserveB. Block-STM must order them sequentially under the hood.
    function swapAToB(uint256 amountAIn) external {
        require(amountAIn > 0, "Amount must be > 0");
        uint256 amountBOut = (reserveB * amountAIn) / (reserveA + amountAIn);
        reserveA += amountAIn;
        reserveB -= amountBOut;
        emit Swap(msg.sender, amountAIn, amountBOut);
    }
}

// ---------------------------------------------------------
// Test 4 & 5: Abort / Rollback / Gas Tracking
// ---------------------------------------------------------
contract AbortRollback {
    uint256 public phase = 1;
    mapping(address => uint256) public userData;

    function setPhase(uint256 p) external {
        phase = p;
    }

    // If phase changes while this is running in parallel, it should revert!
    function updateIfPhase1(uint256 val) external {
        require(phase == 1, "Phase is no longer 1! Reverted!");
        userData[msg.sender] = val;
    }
}

// ---------------------------------------------------------
// Test 1: Update Same Contract
// ---------------------------------------------------------
contract TestCounter {
    uint256 private count;

    // Emitted on each successful increment
    event Incremented(uint256 newCount);

    /// @notice Increment the counter by 1 and emit the new value
    function increment() external {
        count += 1;
        emit Incremented(count);
    }

    /// @notice Read the current counter value (view, no gas)
    /// @notice Read the current counter value (view, no gas)
    function getCount() external view returns (uint256) {
        return count;
    }
}

// ---------------------------------------------------------
// Test 8: Mixed EVM and Native
// ---------------------------------------------------------
contract DepositContract {
    uint256 public totalDeposits;

    function deposit() external payable {
        totalDeposits += msg.value;
    }

    function getTotalDeposits() external view returns (uint256) {
        return totalDeposits;
    }
}
