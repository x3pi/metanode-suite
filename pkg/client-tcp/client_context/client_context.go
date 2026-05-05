package client_context

import (
	"tool-test/pkg/client-tcp/config"
	client_types "tool-test/pkg/client-tcp/types"
	"tool-test/pkg/bls"
	"tool-test/pkg/types/network"
)

type ClientContext struct {
	// config
	Config  *config.ClientConfig
	KeyPair *bls.KeyPair

	// network
	ConnectionsManager network.ConnectionsManager
	MessageSender      network.MessageSender
	SocketServer       network.SocketServer
	Handler            client_types.ClientHandler
}
