// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SimpleChat {
    struct Message {
        address from;
        address to;
        string text;
        uint256 timestamp;
    }

    Message[] public messages;

    event MessageSent(
        address indexed from,
        address indexed to,
        string message,
        uint256 timestamp
    );

    function sendMessage(address _to, string calldata _message, uint256 _clientTimestamp) external {
        messages.push(
            Message({
                from: msg.sender,
                to: _to,
                text: _message,
                timestamp: _clientTimestamp
            })
        );

        emit MessageSent(msg.sender, _to, _message, _clientTimestamp);
    }

    function getMessagesCount() external view returns (uint256) {
        return messages.length;
    }

    function getMessage(
        uint256 _index
    )
        external
        view
        returns (
            address from,
            address to,
            string memory text,
            uint256 timestamp
        )
    {
        require(_index < messages.length, "Index out of bounds");
        Message memory m = messages[_index];
        return (m.from, m.to, m.text, m.timestamp);
    }
}
