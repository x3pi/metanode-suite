// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title CrossChainCaro
 * @dev Smart Contract quản lý Bàn cờ Caro (Tic-Tac-Toe / Gomoku) Xuyên Chuỗi
 * Hỗ trợ đồng bộ trạng thái nước đi giữa các Private Chain qua Public Chain Root Anchor.
 */
contract CrossChainCaro {
    enum Cell { Empty, X, O }
    enum GameStatus { InProgress, X_Won, O_Won, Draw }

    struct Game {
        address playerX; // Người chơi X (Chain 101)
        address playerO; // Người chơi O (Chain 102)
        uint256 wagerAmount; // Tiền cược mỗi bên
        Cell[3][3] board;
        Cell currentTurn;
        GameStatus status;
        uint8 moveCount;
    }

    mapping(uint256 => Game) public games;
    uint256 public nextGameId = 1;

    event GameCreated(uint256 indexed gameId, address playerX, address playerO, uint256 wager);
    event MovePlayed(uint256 indexed gameId, uint8 row, uint8 col, Cell player);
    event GameEnded(uint256 indexed gameId, GameStatus status, address winner);

    function createGame(address playerX, address playerO) external payable returns (uint256) {
        uint256 gameId = nextGameId++;
        Game storage g = games[gameId];
        g.playerX = playerX;
        g.playerO = playerO;
        g.wagerAmount = msg.value;
        g.currentTurn = Cell.X;
        g.status = GameStatus.InProgress;

        emit GameCreated(gameId, playerX, playerO, msg.value);
        return gameId;
    }

    function playMove(uint256 gameId, uint8 row, uint8 col, uint8 playerCell) external returns (bool) {
        Game storage g = games[gameId];
        require(g.status == GameStatus.InProgress, "Game not active");
        require(row < 3 && col < 3, "Invalid coordinates");
        require(g.board[row][col] == Cell.Empty, "Cell already occupied");

        Cell cell = Cell(playerCell);
        require(cell == g.currentTurn, "Not your turn");

        g.board[row][col] = cell;
        g.moveCount++;

        emit MovePlayed(gameId, row, col, cell);

        if (checkWin(gameId, cell)) {
            g.status = (cell == Cell.X) ? GameStatus.X_Won : GameStatus.O_Won;
            address winner = (cell == Cell.X) ? g.playerX : g.playerO;
            emit GameEnded(gameId, g.status, winner);
            return true;
        }

        if (g.moveCount == 9) {
            g.status = GameStatus.Draw;
            emit GameEnded(gameId, GameStatus.Draw, address(0));
            return true;
        }

        g.currentTurn = (cell == Cell.X) ? Cell.O : Cell.X;
        return false;
    }

    function checkWin(uint256 gameId, Cell c) internal view returns (bool) {
        Game storage g = games[gameId];
        // Rows & Columns
        for (uint8 i = 0; i < 3; i++) {
            if (g.board[i][0] == c && g.board[i][1] == c && g.board[i][2] == c) return true;
            if (g.board[0][i] == c && g.board[1][i] == c && g.board[2][i] == c) return true;
        }
        // Diagonals
        if (g.board[0][0] == c && g.board[1][1] == c && g.board[2][2] == c) return true;
        if (g.board[0][2] == c && g.board[1][1] == c && g.board[2][0] == c) return true;
        return false;
    }

    function getBoard(uint256 gameId) external view returns (uint8[3][3] memory boardResult) {
        Game storage g = games[gameId];
        for (uint8 r = 0; r < 3; r++) {
            for (uint8 c = 0; c < 3; c++) {
                boardResult[r][c] = uint8(g.board[r][c]);
            }
        }
        return boardResult;
    }
}
