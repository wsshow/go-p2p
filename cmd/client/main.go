package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go-p2p/internal/config"
	"go-p2p/internal/log"
	"go-p2p/internal/message"
	"go-p2p/internal/peer"
	"go-p2p/pkg/discovery"
	"go-p2p/ui/terminal"
)

// PeerEventAdapter 适配器，将PeerManager与PeerEventListener接口连接
type PeerEventAdapter struct {
	peerManager *peer.PeerManager
}

// OnPeerEvent 实现PeerEventListener接口
func (a *PeerEventAdapter) OnPeerEvent(event discovery.PeerEventData) {
	// 处理对等节点事件
	log.Info("Peer event: %v for peer %s", event.EventType, event.Peer.Name)
}

func main() {
	// 初始化日志
	log.SetLevel(log.INFO)
	log.SetPrefix("[Client] ")
	log.Info("Starting P2P client...")

	// 加载配置
	cfg := config.GetConfig()
	config.LoadFromFile("config.json")

	// 从命令行参数获取客户端名称和服务器地址
	clientName := cfg.Client.Username
	serverAddr := cfg.Client.ServerAddr

	// 解析命令行参数
	// 参数格式: client.exe <client_name> <server_addr>
	if len(os.Args) > 1 {
		clientName = os.Args[1]
		cfg.Client.Username = clientName
	}

	if len(os.Args) > 2 {
		serverAddr = os.Args[2]
		cfg.Client.ServerAddr = serverAddr
	}

	// 验证客户端名称
	if strings.TrimSpace(clientName) == "" {
		log.Fatal("Client name cannot be empty")
	}

	log.Info("Client name: %s", clientName)
	log.Info("Server address: %s", serverAddr)

	// 解析服务器地址
	serverAddrParts := strings.Split(serverAddr, ":")
	serverHost := "127.0.0.1"
	serverPort := cfg.Server.Port

	if len(serverAddrParts) == 2 {
		serverHost = serverAddrParts[0]
		// 尝试解析端口
		if _, err := fmt.Sscanf(serverAddrParts[1], "%d", &serverPort); err != nil {
			log.Warn("Failed to parse port from server address, using default: %v", err)
		}
	} else {
		log.Warn("Invalid server address format, using default: %s:%d", serverHost, serverPort)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		log.Fatal("Failed to resolve server address: %v", err)
	}

	log.Info("Connecting to server at %s", udpAddr.String())

	// 创建节点发现服务
	discoveryService := discovery.NewServerBasedDiscovery(udpAddr, clientName)

	// 创建对等节点管理器
	peerManager := peer.NewPeerManager(clientName)

	// 创建适配器并将其添加为监听器
	peerAdapter := &PeerEventAdapter{peerManager: peerManager}
	// 将节点发现服务与对等节点管理器关联
	discoveryService.AddListener(peerAdapter)

	// 启动节点发现服务
	log.Info("About to start discovery service...")
	err = discoveryService.Start()
	if err != nil {
		log.Fatal("Failed to start discovery service: %v", err)
	}
	defer discoveryService.Stop()

	log.Info("Discovery service started successfully")

	// 创建终端界面
	log.Info("Creating terminal interface...")
	term := terminal.NewTerminal()
	term.SetPromptPrefix(fmt.Sprintf("%s> ", clientName))

	// 注册命令处理函数
	log.Info("Registering commands...")
	registerCommands(term, peerManager, discoveryService)

	// 设置退出处理函数
	term.SetExitHandler(func() {
		log.Info("Exiting...")
		discoveryService.Stop()
		peerManager.Shutdown()
		os.Exit(0)
	})

	// 处理系统信号
	log.Info("Setting up signal handlers...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("Received termination signal")
		discoveryService.Stop()
		peerManager.Shutdown()
		os.Exit(0)
	}()

	log.Info("Starting interactive terminal...")
	// 启动终端
	term.Run()
}

// registerCommands 注册命令处理函数
func registerCommands(term *terminal.Terminal, peerManager *peer.PeerManager, discovery discovery.PeerDiscovery) {
	// 连接到对等节点
	term.AddCommand(terminal.CommandSuggestion{
		Text:        "connectto",
		Description: "Connect to a peer",
	}, func(args []string) error {
		if len(args) < 1 {
			term.PrintError("Usage: connectto <peer_name>")
			return fmt.Errorf("missing peer name")
		}

		peerName := args[0]
		peer, exists := discovery.GetPeer(peerName)
		if !exists {
			term.PrintError("Peer '%s' not found", peerName)
			return fmt.Errorf("peer not found: %s", peerName)
		}

		term.PrintInfo("Connecting to %s (%s)...", peer.Name, peer.Address.String())
		err := peerManager.ConnectToPeer(peer.Name, peer.Address)
		if err != nil {
			term.PrintError("Failed to connect: %v", err)
			return fmt.Errorf("failed to connect: %v", err)
		}

		// 更新对等节点状态
		discovery.UpdatePeerStatus(peer.Name, true)
		term.PrintSuccess("Connected to %s", peer.Name)
		return nil
	})

	// 发送消息
	term.AddCommand(terminal.CommandSuggestion{
		Text:        "msg",
		Description: "Send a message to a peer",
	}, func(args []string) error {
		if len(args) < 2 {
			term.PrintError("Usage: msg <peer_name> <message>")
			return fmt.Errorf("missing arguments")
		}

		peerName := args[0]
		messageText := args[1]

		// 检查是否连接到该对等节点
		if !peerManager.IsPeerConnected(peerName) {
			term.PrintError("Not connected to peer '%s'", peerName)
			return fmt.Errorf("not connected to peer: %s", peerName)
		}

		// 创建消息对象
		msg := message.NewMessage(message.Msg, messageText)

		// 发送消息
		err := peerManager.SendMessage(peerName, msg)
		if err != nil {
			term.PrintError("Failed to send message: %v", err)
			return fmt.Errorf("failed to send message: %v", err)
		}

		term.PrintSuccess("Message sent to %s", peerName)
		return nil
	})

	// 断开连接
	term.AddCommand(terminal.CommandSuggestion{
		Text:        "disconnect",
		Description: "Disconnect from a peer",
	}, func(args []string) error {
		if len(args) < 1 {
			term.PrintError("Usage: disconnect <peer_name>")
			return fmt.Errorf("missing peer name")
		}

		peerName := args[0]

		// 检查是否连接到该对等节点
		if !peerManager.IsPeerConnected(peerName) {
			term.PrintError("Not connected to peer '%s'", peerName)
			return fmt.Errorf("not connected to peer: %s", peerName)
		}

		// 断开连接
		err := peerManager.DisconnectPeer(peerName)
		if err != nil {
			term.PrintError("Failed to disconnect: %v", err)
			return fmt.Errorf("failed to disconnect: %v", err)
		}

		// 更新对等节点状态
		discovery.UpdatePeerStatus(peerName, false)
		term.PrintSuccess("Disconnected from %s", peerName)
		return nil
	})

	// 列出所有对等节点
	term.AddCommand(terminal.CommandSuggestion{
		Text:        "peers",
		Description: "List all available peers",
	}, func(args []string) error {
		peers := discovery.GetPeers()
		if len(peers) == 0 {
			term.PrintInfo("No peers available")
			return nil
		}

		term.PrintInfo("Available peers:")
		for _, peer := range peers {
			status := "Not connected"
			if peer.IsConnected {
				status = "Connected"
			}
			term.PrintInfo("  %s (%s) - %s", peer.Name, peer.Address.String(), status)
		}
		return nil
	})

	// 帮助命令
	term.AddCommand(terminal.CommandSuggestion{
		Text:        "help",
		Description: "Show help message",
	}, func(args []string) error {
		term.PrintInfo("Available commands:")
		term.PrintInfo("  connectto <peer_name>       - Connect to a peer")
		term.PrintInfo("  msg <peer_name> <message>  - Send a message to a peer")
		term.PrintInfo("  disconnect <peer_name>     - Disconnect from a peer")
		term.PrintInfo("  peers                      - List all available peers")
		term.PrintInfo("  help                       - Show this help message")
		term.PrintInfo("  exit                       - Exit the application")
		return nil
	})
}
