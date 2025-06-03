package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go-p2p/storage"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Client 封装了客户端的所有状态和方法 (encapsulates all client-specific state and methods)
type Client struct {
	serverConn   *net.UDPConn        // 与服务器的UDP连接 (UDP connection to the server)
	localUDPAddr *net.UDPAddr        // 用于服务器通信的本地UDP地址 (Local UDP address for server communication)
	clientName   string              // 服务器分配的客户端名称 (Client name assigned by the server)
	p2pManager   *P2PConnectionManager // P2P连接管理器 (P2P connection manager)
	commands     map[string]Command  // 客户端可用命令 (Available client commands)

	isShuttingDown bool                // 标记客户端是否正在关闭 (Flag indicating if the client is shutting down)
	shutdownMutex  sync.Mutex          // 用于保护isShuttingDown的互斥锁 (Mutex to protect isShuttingDown)
}

// NewClient 创建并初始化一个新的Client实例 (creates and initializes a new Client instance)
func NewClient() *Client {
	c := &Client{
		p2pManager: NewP2PConnectionManager(), // 初始化P2P管理器 (Initialize P2P manager)
	}
	c.initCommands() // 初始化命令 (Initialize commands)
	// 为P2P管理器提供获取客户端名称的方法 (Provide P2PManager with a way to get client's name)
	// This ensures P2PManager can access the most current clientName for things like Hello messages.
	c.p2pManager.SetClientNameProvider(func() string { return c.clientName })
	return c
}

// Start 初始化并运行客户端操作,包括连接服务器和启动处理协程
// (initializes and runs the client operations, including connecting to the server and starting processing goroutines)
func (c *Client) Start(clientPort int, serverAddr string) {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
	log.SetPrefix("[go-p2p-client] ") // 设置日志前缀,方便区分客户端日志 (Set log prefix for easy client log identification)

	saddr, err := net.ResolveUDPAddr("udp4", serverAddr) // 解析服务器地址 (Resolve server address)
	if err != nil {
		log.Fatalf("[FATAL] Error resolving server address '%s': %v", serverAddr, err)
	}
	// 如果clientPort为0,系统会自动选择一个可用端口 (If clientPort is 0, the system will auto-select an available port)
	c.localUDPAddr = &net.UDPAddr{IP: net.IPv4zero, Port: clientPort}

	conn, err := net.DialUDP("udp4", c.localUDPAddr, saddr) // 连接到服务器 (Connect to the server)
	if err != nil {
		log.Fatalf("[FATAL] Failed to connect to server %s from local port %d: %v", serverAddr, clientPort, err)
	}
	c.serverConn = conn

	// 获取并记录实际使用的本地地址 (Get and record the actual local address used)
	if actualLocal, ok := c.serverConn.LocalAddr().(*net.UDPAddr); ok {
        c.localUDPAddr = actualLocal // 更新为实际绑定的地址 (Update to the actually bound address)
    } else {
        log.Fatalf("[FATAL] Could not assert LocalAddr to *net.UDPAddr for server connection.")
    }
	log.Printf("[INFO] Connection attempt to server at %s from local UDP address: %s\n", serverAddr, c.localUDPAddr.String())

	// 发送初始连接请求 (Send initial connection request)
	err = c.sendToServer(storage.UserMsg{MsgType: storage.Connect})
	if err != nil {
		log.Fatalf("[FATAL] Failed to send initial connect message to server: %v", err)
	}
	fmt.Println("Sent registration request to server. Waiting for name assignment...") // 等待服务器分配名称 (Waiting for name assignment from server)

	go c.receiveServerMessages() // 启动协程接收服务器消息 (Start goroutine to receive server messages)
	go c.processUserCommands()   // 启动协程处理用户输入 (Start goroutine to process user commands)

	select {} // 阻塞主协程, 直到 os.Exit 被调用 (Block main goroutine until os.Exit is called)
}

// --- P2PConnectionManager ---
// P2PConnectionManager 封装了P2P连接的状态和逻辑
// (P2PConnectionManager encapsulates P2P connection state and logic)
type P2PConnectionManager struct {
	udpConn             *net.UDPConn   // 当前的UDP P2P连接 (Current UDP P2P connection)
	tcpConn             *net.TCPConn   // 当前的TCP P2P连接 (Current TCP P2P connection)
	peerUDPAddr         *net.UDPAddr   // 对端的UDP地址 (从服务器消息解析) (Peer's UDP address, resolved from server message)
	localUDPAddrForPeer *net.UDPAddr   // 我们用于P2P的UDP地址 (由服务器告知对端) (Our UDP address for P2P, known to peer via server)

	mutex           sync.RWMutex   // 保护对连接状态的并发访问 (Mutex to protect concurrent access to connection state)
	isActive        bool           // 指示是否有P2P连接处于活动状态 (Indicates if any P2P connection is active)
	currentProtocol string         // 当前P2P协议: "UDP", "TCP", 或 "" (Current P2P protocol: "UDP", "TCP", or "")
	clientNameProvider func() string // 用于获取当前客户端名称的回调 (Callback to get current client name)
}

// NewP2PConnectionManager 创建一个新的P2P连接管理器实例
// (NewP2PConnectionManager creates a new P2PConnectionManager instance)
func NewP2PConnectionManager() *P2PConnectionManager { return &P2PConnectionManager{} } // 初始化字段为零值 (Initializes fields to their zero values)

// SetClientNameProvider 设置一个函数, P2P管理器可以通过它获取当前的客户端名称
// (SetClientNameProvider sets a function through which the P2P manager can get the current client name)
func (pm *P2PConnectionManager) SetClientNameProvider(provider func() string) { pm.clientNameProvider = provider }

// IsActive 检查P2P连接当前是否处于活动状态
// (IsActive checks if a P2P connection is currently active)
func (pm *P2PConnectionManager) IsActive() bool { pm.mutex.RLock(); defer pm.mutex.RUnlock(); return pm.isActive }

// Protocol 返回当前P2P连接使用的协议 ("UDP", "TCP", 或 "")
// (Protocol returns the protocol used by the current P2P connection: "UDP", "TCP", or "")
func (pm *P2PConnectionManager) Protocol() string { pm.mutex.RLock(); defer pm.mutex.RUnlock(); return pm.currentProtocol }

// RemoteAddr 返回当前活动P2P连接的对端地址
// (RemoteAddr returns the remote address of the currently active P2P connection)
func (pm *P2PConnectionManager) RemoteAddr() net.Addr {
	pm.mutex.RLock(); defer pm.mutex.RUnlock();
	if pm.tcpConn != nil { return pm.tcpConn.RemoteAddr(); }
	if pm.udpConn != nil { return pm.udpConn.RemoteAddr(); }
	return nil // 没有活动连接 (No active connection)
}
// Close 关闭任何活动的P2P连接并重置管理器状态
// (Close terminates any active P2P connection and resets the manager's state)
func (pm *P2PConnectionManager) Close() {
	pm.mutex.Lock(); defer pm.mutex.Unlock();
	closedSomething := false // 标记是否实际关闭了连接 (Flag if any connection was actually closed)
	if pm.udpConn != nil { log.Println("[INFO] P2PManager: Closing UDP P2P connection."); pm.udpConn.Close(); pm.udpConn = nil; closedSomething = true }
	if pm.tcpConn != nil { log.Println("[INFO] P2PManager: Closing TCP P2P connection."); pm.tcpConn.Close(); pm.tcpConn = nil; closedSomething = true }
	pm.isActive = false; pm.currentProtocol = "";
	if closedSomething { log.Println("[INFO] P2PManager: All P2P connections closed and state reset."); }
}
// EstablishUDP 尝试建立UDP P2P连接
// localAddrForPeer 是服务器视角下我们的地址, peerAddr 是服务器视角下对端的地址
// (EstablishUDP attempts to establish a UDP P2P connection.)
// (localAddrForPeer is our address as seen by the server, peerAddr is the peer's address as seen by the server.)
func (pm *P2PConnectionManager) EstablishUDP(localAddrForPeer, peerAddr *net.UDPAddr) error {
	pm.mutex.Lock(); if pm.isActive { pm.mutex.Unlock(); log.Println("[WARN] P2PManager: EstablishUDP called while active. Closing existing."); pm.Close(); pm.mutex.Lock(); }
	pm.peerUDPAddr = peerAddr; pm.localUDPAddrForPeer = localAddrForPeer;
	log.Printf("[INFO] P2PManager: Attempting UDP P2P. Our external: %s, Peer external: %s\n", localAddrForPeer.String(), peerAddr.String())
	// 从服务器告知的、我们自己的“外部”地址和端口进行Dial, 这对NAT打洞至关重要
	// (Dial from our own "external" address and port as told by the server, crucial for NAT hole punching)
	dialConn, err := net.DialUDP("udp4", localAddrForPeer, peerAddr)
	if err != nil {
		log.Printf("[WARN] P2PManager: Failed to dial UDP from specific local %s: %v. Trying general dial.\n", localAddrForPeer.String(), err)
		dialConn, err = net.DialUDP("udp4", nil, peerAddr) // 备选方案:不指定本地地址 (Fallback: do not specify local address)
		if err != nil { pm.mutex.Unlock(); log.Printf("[ERROR] P2PManager: General UDP dial to peer %s also failed: %v\n", peerAddr.String(), err); return fmt.Errorf("failed to establish UDP P2P with %s: %v", peerAddr.String(), err); }
	}
	pm.udpConn = dialConn; pm.isActive = true; pm.currentProtocol = "UDP"; pm.mutex.Unlock();
	log.Printf("[INFO] P2PManager: UDP P2P connection attempt made to %s (using local: %s).\n", pm.udpConn.RemoteAddr().String(), pm.udpConn.LocalAddr().String())
	go pm.receiveUDPPackets() // 启动UDP接收协程 (Start UDP receiver goroutine)
	// 发送几条初始的"Hello"消息以帮助打洞并确认连接
	// (Send a few initial "Hello" messages to aid hole punching and confirm connection)
	go func() {
		var currentClientName = "Unknown"; if pm.clientNameProvider != nil { currentClientName = pm.clientNameProvider() } // 安全获取客户端名称 (Safely get client name)
		for i := 0; i < 3; i++ {
			if !pm.IsActive() || pm.Protocol() != "UDP" { break } // 连接状态检查 (Connection status check)
			msgContent := fmt.Sprintf("(UDP P2P Hello from %s - Attempt %d)", currentClientName, i+1)
			if err := pm.SendMessage(storage.UserMsg{MsgType: storage.Msg, Msg: msgContent}); err != nil { log.Printf("[WARN] P2PManager: Failed to send UDP Hello %d: %v\n", i+1, err); break; }
			time.Sleep(300 * time.Millisecond)
		}
	}()
	return nil
}
// SendMessage 通过当前活动的P2P连接 (UDP或TCP) 发送UserMsg
// (SendMessage sends a UserMsg over the currently active P2P connection (UDP or TCP))
func (pm *P2PConnectionManager) SendMessage(msg storage.UserMsg) error {
	pm.mutex.RLock(); if !pm.isActive { pm.mutex.RUnlock(); return fmt.Errorf("no active P2P connection (没有活动的P2P连接)"); }
	protocol := pm.currentProtocol; var conn io.Writer; // 使用io.Writer接口以支持不同连接类型 (Use io.Writer interface to support different connection types)
	if protocol == "UDP" && pm.udpConn != nil { conn = pm.udpConn }
	if protocol == "TCP" && pm.tcpConn != nil { conn = pm.tcpConn }
	pm.mutex.RUnlock(); if conn == nil { return fmt.Errorf("no active P2P conn for protocol %s (协议 %s 没有活动的P2P连接)", protocol, protocol) }
	bs, err := json.Marshal(msg); if err != nil { log.Printf("[ERROR] P2PManager: Marshal P2P msg err: %v\n", err); return err; }
	if protocol == "TCP" { bs = append(bs, '\n') } // TCP消息使用换行符分隔 (TCP messages use newline as delimiter)
	_, err = conn.Write(bs); if err != nil { log.Printf("[ERROR] P2PManager: Send %s P2P err: %v\n", protocol, err); }
	return err
}
// receiveUDPPackets 是一个goroutine, 用于读取传入的P2P UDP数据包
// (receiveUDPPackets is a goroutine for reading incoming P2P UDP packets)
func (pm *P2PConnectionManager) receiveUDPPackets() {
	b := make([]byte, 2048); for {
		pm.mutex.RLock(); if pm.udpConn == nil || !pm.isActive || pm.currentProtocol != "UDP" { pm.mutex.RUnlock(); return; } // 状态检查 (State check)
		udpC := pm.udpConn; pm.mutex.RUnlock(); n, remoteAddr, err := udpC.ReadFromUDP(b) // 读取数据 (Read data)
		if err != nil { pm.Close(); if strings.Contains(err.Error(), "use of closed network connection") { log.Println("[INFO] P2PManager: UDP P2P conn closed (receiver)."); } else { log.Printf("[ERROR] P2PManager: Read P2P UDP err: %v\n", err); }; return; } // 错误处理和关闭 (Error handling and closure)
		pm.processIncomingP2PPacket(b, n, remoteAddr, "UDP") // 处理包 (Process packet)
	}
}
// receiveTCPPackets 是一个goroutine, 用于读取传入的P2P TCP数据
// (receiveTCPPackets is a goroutine for reading incoming P2P TCP data)
func (pm *P2PConnectionManager) receiveTCPPackets() {
	pm.mutex.RLock(); if pm.tcpConn == nil || !pm.isActive || pm.currentProtocol != "TCP" { pm.mutex.RUnlock(); return; } // 状态检查 (State check)
	tcpC := pm.tcpConn; pm.mutex.RUnlock(); reader := bufio.NewReader(tcpC); for {
		jsonBytes, err := reader.ReadBytes('\n') // 按行读取 (Read line by line)
		if err != nil { pm.Close(); if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") { log.Println("[INFO] P2PManager: TCP P2P conn closed (receiver)."); } else { log.Printf("[ERROR] P2PManager: Read P2P TCP stream err: %v\n", err); }; return; } // 错误处理和关闭 (Error handling and closure)
		pm.processIncomingP2PPacket(jsonBytes, len(jsonBytes), tcpC.RemoteAddr(), "TCP") // 处理包 (Process packet)
	}
}
// SwitchToTCP 处理将P2P连接从UDP切换到TCP的逻辑
// initiator 为 true 表示此客户端是 'tcp' 命令的发起者
// (SwitchToTCP handles the logic of switching the P2P connection from UDP to TCP.)
// (initiator being true means this client is the originator of the 'tcp' command.)
func (pm *P2PConnectionManager) SwitchToTCP(initiator bool) error {
	pm.mutex.Lock(); if pm.currentProtocol != "UDP" || pm.udpConn == nil { pm.mutex.Unlock(); return fmt.Errorf("no active UDP P2P to switch (没有活动的UDP P2P连接可切换)"); }
	if pm.peerUDPAddr == nil || pm.localUDPAddrForPeer == nil { pm.mutex.Unlock(); return fmt.Errorf("P2P UDP addr info incomplete (P2P UDP地址信息不完整)"); }
	log.Println("[INFO] P2PManager: Starting TCP switch. Initiator:", initiator);
	localAddrForTCP := &net.TCPAddr{IP: pm.localUDPAddrForPeer.IP, Port: pm.localUDPAddrForPeer.Port}; // 本地TCP地址 (Local TCP address)
	peerAddrForTCP := &net.TCPAddr{IP: pm.peerUDPAddr.IP, Port: pm.peerUDPAddr.Port}; // 对端TCP地址 (Peer TCP address)
	if pm.udpConn != nil { pm.udpConn.Close(); pm.udpConn = nil; }; pm.isActive = false; pm.currentProtocol = ""; pm.mutex.Unlock();
	time.Sleep(1 * time.Second); var tempTCPConn *net.TCPConn; var err error;
	if initiator { // 作为发起方 (As initiator)
		log.Printf("[INFO] P2PManager (Initiator): Dialing TCP from %s to %s\n", localAddrForTCP, peerAddrForTCP);
		dialer := net.Dialer{Timeout: 7 * time.Second, LocalAddr: localAddrForTCP}; tempTCPConn, err = dialer.Dial("tcp4", peerAddrForTCP.String())
		if err != nil { // 拨号失败, 尝试监听 (Dial failed, try listening)
			log.Printf("[WARN] P2PManager (Initiator): TCP Dial failed: %v. Listening...\n", err);
			listener, listenErr := net.ListenTCP("tcp4", localAddrForTCP); if listenErr != nil { return fmt.Errorf("initiator TCP dial fail and listen fail: %v, %v (发起方拨号和监听均失败)", err, listenErr); }
			defer listener.Close(); log.Printf("[INFO] P2PManager (Initiator): Listening on %s for 7s...\n", localAddrForTCP);
			listener.SetDeadline(time.Now().Add(7 * time.Second)); tempTCPConn, err = listener.AcceptTCP();
			if err != nil { return fmt.Errorf("initiator TCP accept failed: %v (发起方接受TCP连接失败)", err); }
			log.Println("[INFO] P2PManager (Initiator): Accepted TCP after failed dial.");
		} else { log.Println("[INFO] P2PManager (Initiator): Successfully dialed TCP."); }
	} else {  // 作为接收方 (As receiver)
		log.Printf("[INFO] P2PManager (Receiver): Listening on %s for TCP from %s (10s timeout)\n", localAddrForTCP, peerAddrForTCP);
		listener, listenErr := net.ListenTCP("tcp4", localAddrForTCP); if listenErr != nil { return fmt.Errorf("receiver TCP listen fail: %v (接收方监听TCP失败)", listenErr); }
		defer listener.Close(); listener.SetDeadline(time.Now().Add(10 * time.Second)); tempTCPConn, err = listener.AcceptTCP();
		if err != nil { return fmt.Errorf("receiver TCP accept fail: %v (接收方接受TCP连接失败)", err); }
		log.Println("[INFO] P2PManager (Receiver): Successfully accepted TCP.");
	}
	pm.mutex.Lock(); pm.tcpConn = tempTCPConn; pm.isActive = true; pm.currentProtocol = "TCP"; pm.mutex.Unlock();
	log.Printf("[INFO] P2PManager: Switched to TCP with %s.\n", pm.RemoteAddr().String()); go pm.receiveTCPPackets(); return nil;
}
// processIncomingP2PPacket 处理通过活动的P2P连接收到的消息 (UDP或TCP)
// (processIncomingP2PPacket handles messages received over an active P2P connection (UDP or TCP))
func (pm *P2PConnectionManager) processIncomingP2PPacket(rawMsg []byte, n int, remoteAddr net.Addr, connType string) {
	var usermsg storage.UserMsg; dataToUnmarshal := rawMsg; if connType != "TCP" { dataToUnmarshal = rawMsg[:n]; } // TCP的rawMsg已是完整消息 (For TCP, rawMsg is already the complete message)
	if err := json.Unmarshal(dataToUnmarshal, &usermsg); err != nil { log.Printf("[WARN] P2PManager: Unmarshal P2P %s msg err: %v. Data: %s\n", connType, err, string(dataToUnmarshal)); return; }
	log.Printf("[INFO] P2PManager: Got P2P %s from %s: Type %d, Msg '%s'\n", connType, remoteAddr, usermsg.MsgType, usermsg.Msg)
	switch usermsg.MsgType {
	case storage.Msg: fmt.Printf("\n[%s %s]: %s\n", connType, remoteAddr.String(), usermsg.Msg); // Client handles printing prompt
	case storage.ChangeToTCP: // 对端请求切换到TCP (Peer requests switch to TCP)
		if connType != "UDP" { log.Printf("[WARN] P2PManager: ChangeToTCP not on UDP. Ignoring.\n"); return; }
		log.Printf("[INFO] P2PManager: Peer %s requests TCP switch. Handling as receiver.\n", remoteAddr.String());
		if err := pm.SwitchToTCP(false); err != nil { log.Printf("[ERROR] P2PManager: Error switching to TCP as receiver: %v\n", err); } // 作为接收方切换 (Switch as receiver)
	default: log.Printf("[WARN] P2PManager: Unhandled P2P msg type %d from %s\n", usermsg.MsgType, remoteAddr.String());
	}
}

// --- Command Definitions & Handlers (methods of *Client) ---
// (命令定义和处理函数, 现在是 Client 结构体的方法 - Command definitions and handlers, now methods of the Client struct)

// Command 定义客户端命令的结构 (defines the structure for a client command)
type Command struct {
	Description string                             // 命令的简短描述 (Short description of the command)
	Usage       string                             // 命令的使用格式 (Usage format of the command)
	Handler     func(c *Client, args []string) error // 命令处理函数, 接收 *Client 作为参数 (Handler function, receives *Client as parameter)
}

// initCommands 初始化客户端支持的命令 (initializes the commands supported by the client)
// 此方法在 NewClient 中调用 (This method is called in NewClient)
func (c *Client) initCommands() {
	c.commands = map[string]Command{
		"all":    { Description: "List all connected clients. (列出所有已连接客户端)", Usage: "all", Handler: (*Client).handleListAll },
		"connect":{ Description: "Request to connect to another client by name. (请求连接到指定名称的客户端)", Usage: "connect <client_name>", Handler: (*Client).handleConnectTo },
		"allow":  { Description: "Allow a connection request from another client. (允许来自其他客户端的连接请求)", Usage: "allow <client_name>", Handler: (*Client).handleAllowConnect },
		"deny":   { Description: "Deny a connection request from another client. (拒绝来自其他客户端的连接请求)", Usage: "deny <client_name>", Handler: (*Client).handleDenyConnect },
		"msg":    { Description: "Send a message to the connected peer. (发送消息给已连接的对端)", Usage: "msg <message_content>", Handler: (*Client).handleSendMessage },
		"rename": { Description: "Request to change your client name on the server. (请求在服务器上更改您的客户端名称)", Usage: "rename <new_name>", Handler: (*Client).handleRename },
		"tcp":    { Description: "Switch the current P2P UDP connection to TCP. (将当前的P2P UDP连接切换到TCP)", Usage: "tcp", Handler: (*Client).handleChangeToTCP },
		"help":   { Description: "Show this help message. (显示此帮助信息)", Usage: "help", Handler: (*Client).handleHelp },
		"exit":   { Description: "Close connections and exit the client. (关闭连接并退出客户端)", Usage: "exit", Handler: (*Client).handleExit },
	}
}

// handleHelp 显示所有可用命令及其用法 (displays all available commands and their usage)
func (c *Client) handleHelp(args []string) error { fmt.Println("\nAvailable commands (可用命令):"); for name, cmd := range c.commands { fmt.Printf("  %s: %s\n    Usage (用法): %s\n", name, cmd.Description, cmd.Usage); }; fmt.Println(); return nil; }

// handleListAll 请求服务器列出所有客户端 (requests the server to list all clients)
func (c *Client) handleListAll(args []string) error { if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }; return c.sendToServer(storage.UserMsg{MsgType: storage.SearchAll}); }

// handleConnectTo 请求连接到另一个客户端 (requests to connect to another client)
func (c *Client) handleConnectTo(args []string) error { if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }; if len(args) < 1 { return fmt.Errorf("usage (用法): connect <client_name>"); }; targetName := args[0]; if targetName == c.clientName { return fmt.Errorf("cannot connect to yourself (不能连接到自己)"); }; fmt.Printf("Requesting connection to %s...\n", targetName); return c.sendToServer(storage.UserMsg{MsgType: storage.ConnectTo, Msg: targetName}); }

// handleAllowConnect 允许一个P2P连接请求 (allows a P2P connection request)
func (c *Client) handleAllowConnect(args []string) error { if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }; if len(args) < 1 { return fmt.Errorf("usage (用法): allow <client_name_who_requested>"); }; requesterName := args[0]; err := c.sendToServer(storage.UserMsg{MsgType: storage.ConnectAllow, Msg: requesterName}); if err != nil { return fmt.Errorf("failed to send allow command: %w", err); }; fmt.Printf("Sent allow confirmation for %s to server.\n", requesterName); return nil; }

// handleDenyConnect 拒绝一个P2P连接请求 (denies a P2P connection request)
func (c *Client) handleDenyConnect(args []string) error { if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }; if len(args) < 1 { return fmt.Errorf("usage (用法): deny <client_name_who_requested>"); }; requesterName := args[0]; fmt.Printf("Sending deny for %s to server.\n", requesterName); return c.sendToServer(storage.UserMsg{MsgType: storage.ConnectDeny, Msg: requesterName}); }

// handleRename 请求更改客户端名称 (requests to change the client name)
func (c *Client) handleRename(args []string) error { if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }; if len(args) < 1 { return fmt.Errorf("usage (用法): rename <new_name>"); }; newName := args[0]; if strings.TrimSpace(newName) == "" {return fmt.Errorf("new name cannot be empty (新名称不能为空)");}; fmt.Printf("Requesting rename to '%s'...\n", newName); return c.sendToServer(storage.UserMsg{MsgType: storage.Rename, Msg: newName}); }

// handleSendMessage 通过P2P连接发送消息 (sends a message over the P2P connection)
func (c *Client) handleSendMessage(args []string) error {
	if len(args) < 1 { return fmt.Errorf("usage (用法): msg <message_content>"); }
	message := strings.Join(args, " ");
	// 确保P2P管理器已初始化且连接活跃 (Ensure P2PManager is initialized and connection is active)
	if c.p2pManager == nil || !c.p2pManager.IsActive() { return fmt.Errorf("not connected to any peer (P2P管理器未激活或未连接) (not connected to any peer)"); }
	fmt.Printf("[Me -> %s %s]: %s\n", c.p2pManager.Protocol(), c.p2pManager.RemoteAddr().String(), message);
	return c.p2pManager.SendMessage(storage.UserMsg{MsgType: storage.Msg, Msg: message});
}

// handleChangeToTCP 请求将P2P连接切换到TCP (requests to switch the P2P connection to TCP)
func (c *Client) handleChangeToTCP(args []string) error {
	// 检查P2P管理器和连接状态 (Check P2PManager and connection state)
	if c.p2pManager == nil || !c.p2pManager.IsActive() || c.p2pManager.Protocol() != "UDP" { return fmt.Errorf("no active UDP P2P connection to switch (没有活动的UDP P2P连接可切换)"); }
	log.Println("[INFO] CMD: User requested TCP switch. Sending ChangeToTCP msg via UDP.");
	// 1. 首先通过UDP P2P连接发送ChangeToTCP控制消息 (1. First send ChangeToTCP control message via current UDP P2P connection)
	err := c.p2pManager.SendMessage(storage.UserMsg{MsgType: storage.ChangeToTCP})
	if err != nil { return fmt.Errorf("failed to send ChangeToTCP request to peer via UDP: %v", err); }
	// 2. 然后调用P2P管理器的SwitchToTCP方法, 作为发起方 (2. Then call P2PManager's SwitchToTCP method as initiator)
	return c.p2pManager.SwitchToTCP(true);
}

// handleExit 关闭所有连接并安全退出客户端 (closes all connections and safely exits the client)
func (c *Client) handleExit(args []string) error {
	c.shutdownMutex.Lock(); // 获取关闭锁 (Acquire shutdown lock)
	if c.isShuttingDown { c.shutdownMutex.Unlock(); return nil; } // 防止重复关闭 (Prevent double shutdown)
	c.isShuttingDown = true; // 设置关闭标记 (Set shutdown flag)
	c.shutdownMutex.Unlock();

	log.Println("[INFO] Exiting client application...");
	if c.serverConn != nil { c.serverConn.Close(); c.serverConn = nil; } // 关闭与服务器的连接 (Close connection to server)
	if c.p2pManager != nil { c.p2pManager.Close(); } // 关闭P2P连接 (Close P2P connection)
	fmt.Println("Connections closed. Bye!"); os.Exit(0); return nil; // 退出程序 (Exit program)
}

// --- Client Methods for Network and Commands ---
// (客户端网络和命令处理方法)

// sendToServer 向服务器发送UserMsg类型的消息 (sends a UserMsg type message to the server)
func (c *Client) sendToServer(msg storage.UserMsg) error {
	if c.serverConn == nil { return fmt.Errorf("not connected to server (未连接到服务器)"); }
	bs, err := json.Marshal(msg); if err != nil { log.Printf("[ERROR] Failed to marshal server message %+v: %v\n", msg, err); return fmt.Errorf("failed to marshal server message: %v (序列化消息失败)", err); }
	_, err = c.serverConn.Write(bs);
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "network is unreachable") { log.Printf("[ERROR] Error sending to server: Connection refused or network unreachable.\n"); } else { log.Printf("[ERROR] Failed to send message to server: %v\n", err); }
		return fmt.Errorf("failed to send message to server: %v (发送消息失败)", err);
	}
	return nil;
}

// getCurrentPrompt 生成并返回当前的命令行提示符, 反映客户端状态 (generates and returns the current command line prompt, reflecting client state)
func (c *Client) getCurrentPrompt() string {
	namePart := ""; if c.clientName != "" { namePart = fmt.Sprintf("[%s]", c.clientName); } // 显示客户端名称 (Display client name)
	connPart := "";
	// 如果P2P管理器存在且P2P连接活跃, 显示P2P连接信息 (If P2PManager exists and P2P connection is active, display P2P info)
	if c.p2pManager != nil && c.p2pManager.IsActive() {
		remoteAddr := c.p2pManager.RemoteAddr()
		if remoteAddr != nil { connPart = fmt.Sprintf("[%s %s]", c.p2pManager.Protocol(), remoteAddr.String()); }
	}
	if namePart == "" && connPart == "" { return "> "; } // 默认提示符 (Default prompt)
	return fmt.Sprintf("%s%s> ", namePart, connPart);
}

// processUserCommands 循环处理用户在命令行输入的命令 (loops to process user commands from the command line)
func (c *Client) processUserCommands() {
	reader := bufio.NewReader(os.Stdin);
	// 短暂休眠, 允许其他初始化消息 (如服务器分配的名称) 先行打印 (Brief sleep to allow other initial messages, like server-assigned name, to print first)
	time.Sleep(time.Millisecond * 200);
	for {
		c.shutdownMutex.Lock(); isDown := c.isShuttingDown; c.shutdownMutex.Unlock();
		if isDown { log.Println("[INFO] processUserCommands: Shutting down, command input loop terminating."); return; } // 检查关闭标记 (Check shutdown flag)

		fmt.Print(c.getCurrentPrompt()); // 显示当前提示符 (Display current prompt)
		input, err := reader.ReadString('\n'); // 读取用户输入直到换行 (Read user input until newline)
		if err != nil {
			if err == io.EOF { log.Println("[INFO] EOF detected from stdin, initiating exit."); c.handleExit(nil); return; }; // 处理Ctrl+D (Handle Ctrl+D)
			log.Printf("[WARN] Error reading input from stdin: %v\n", err); continue;
		}
		input = strings.TrimSpace(input); if input == "" { continue; } // 忽略空行 (Ignore empty lines)
		parts := strings.Fields(input); cmdName := strings.ToLower(parts[0]); args := parts[1:]; // 解析命令和参数 (Parse command and arguments)
		// 查找并执行命令对应的处理函数 (Find and execute the handler for the command)
		if cmd, exists := c.commands[cmdName]; exists {
			if err := cmd.Handler(c, args); err != nil { fmt.Printf("Error (错误): %v\n", err); } // 执行处理器并打印错误 (Execute handler and print errors)
		} else { fmt.Printf("Unknown command (未知命令): %s. Type 'help' for commands.\n", cmdName); }
	}
}

// receiveServerMessages 持续监听并处理从服务器接收到的消息 (continuously listens for and processes messages received from the server)
func (c *Client) receiveServerMessages() {
	b := make([]byte, 2048); var usermsg storage.UserMsg; // 消息缓冲区和UserMsg结构体 (Message buffer and UserMsg struct)
	for {
		c.shutdownMutex.Lock(); isDown := c.isShuttingDown; c.shutdownMutex.Unlock();
		// 如果正在关闭或服务器连接已断开, 则终止协程 (If shutting down or server connection is lost, terminate goroutine)
		if isDown || c.serverConn == nil { log.Println("[INFO] receiveServerMessages: Shutting down or serverConn is nil. Terminating goroutine."); return; }

		// 为ReadFromUDP设置超时以允许定期检查关闭状态 (Set a deadline for ReadFromUDP to allow periodic checks of shutdown status)
		if err := c.serverConn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
            // 在高频调用下, 此日志可能过于频繁, 可以考虑移除或设为DEBUG级别
            // (This log might be too frequent under high load, consider removing or setting to DEBUG level)
            // log.Printf("[WARN] Failed to set read deadline for server conn: %v\n", err);
        }
		n, remoteAddr, err := c.serverConn.ReadFromUDP(b);
        c.serverConn.SetReadDeadline(time.Time{}); // 成功读取或超时后清除超时设置 (Clear deadline after successful read or timeout)

		if err != nil { // 处理读取错误 (Handle read errors)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                continue; // 读取超时是预期的, 继续循环以检查关闭状态 (Read timeout is expected, continue loop to check shutdown status)
            }
			// 如果连接已关闭 (例如由handleExit关闭), 则记录并终止协程 (If connection closed, e.g. by handleExit, log and terminate goroutine)
			if strings.Contains(err.Error(), "use of closed network connection") {
				log.Println("[INFO] Server connection closed, receiveServerMessages goroutine terminating.");
				// 通常handleExit会处理所有清理, 这里确保如果因其他原因关闭也能优雅退出 (Usually handleExit handles all cleanup, this ensures graceful exit if closed for other reasons)
				// c.handleExit(nil) // Avoid recursive call if shutdown already in progress
				return;
			}
			log.Printf("[WARN] Error reading from server UDP (remote: %s): %v\n", remoteAddr, err);
			time.Sleep(1 * time.Second); // 发生其他错误时短暂暂停 (Brief pause on other errors)
			continue;
		}
		// 反序列化收到的消息 (Deserialize received message)
		if err := json.Unmarshal(b[:n], &usermsg); err != nil { log.Printf("[WARN] Error unmarshalling message from server %s: %v. Data: %s\n", remoteAddr, err, string(b[:n])); continue; }
		c.processIncomingMessageFromServer(usermsg, remoteAddr); // 调用内部方法处理具体消息 (Call internal method to process specific message)
	}
}

// processIncomingMessageFromServer 根据消息类型处理从服务器接收到的具体消息
// (processIncomingMessageFromServer processes specific messages received from the server based on its type)
func (c *Client) processIncomingMessageFromServer(usermsg storage.UserMsg, remoteAddr net.Addr) {
	// 在打印来自服务器的消息前, 清理或覆盖当前行上的提示符, 以免消息显示混乱
	// (Before printing messages from the server, clear or overwrite the prompt on the current line to avoid messy display)
	currentPromptText := c.getCurrentPrompt()
	// 使用回车符(CR)将光标移到行首,然后用空格覆盖旧提示符,再用回车符回到行首准备打印新消息
	// (Use carriage return (CR) to move cursor to line start, then overwrite old prompt with spaces, then CR again for new message)
	fmt.Printf("\r%s\r", strings.Repeat(" ", len(currentPromptText)+2))

	switch usermsg.MsgType {
	case storage.Heartbeat: // 服务器心跳请求: 直接回显收到的消息内容 (Server heartbeat request: echo back the received message content)
		if err := c.sendToServer(storage.UserMsg{MsgType: storage.Heartbeat, Msg: usermsg.Msg}); err != nil { log.Printf("[ERROR] Failed to send heartbeat response: %v\n", err); }
	case storage.ConnectTo: // 其他客户端请求连接的通知 (Notification of a P2P connection request from another client)
		peerNameRequesting := usermsg.Msg;
		fmt.Printf("Incoming connection request from '%s'. (收到来自 '%s' 的连接请求)\nTo accept (接受): allow %s\nTo deny (拒绝): deny %s\n", peerNameRequesting, peerNameRequesting, peerNameRequesting, peerNameRequesting);
	case storage.ConnectAllow: // 服务器确认P2P连接请求已被对端允许, 并提供了双方的公网地址信息
	                           // (Server confirms P2P connection request was allowed by peer, providing public address info for both)
		parts := strings.Split(usermsg.Msg, ","); if len(parts) != 2 { log.Printf("[ERROR] Invalid ConnectAllow from server: %s\n", usermsg.Msg); break; }
		peerAddrStr, myAddrStr := parts[0], parts[1]; // peerAddrStr是对端地址, myAddrStr是服务器看到的我方地址 (peerAddrStr is peer's, myAddrStr is ours as seen by server)

		rPeerAddr, errP := net.ResolveUDPAddr("udp4", peerAddrStr); rMyAddr, errM := net.ResolveUDPAddr("udp4", myAddrStr); // 解析地址 (Resolve addresses)
		if errP != nil || errM != nil { log.Printf("[ERROR] Resolve Addr ConnectAllow: PeerErr %v, MyErr %v\n", errP, errM); break; }

		fmt.Printf("Server authorized P2P. Peer: %s, Your external: %s\n", rPeerAddr.String(), rMyAddr.String());
		// 调用P2P管理器建立UDP连接 (Call P2PManager to establish UDP connection)
		if err := c.p2pManager.EstablishUDP(rMyAddr, rPeerAddr); err != nil {
			log.Printf("[ERROR] Establish UDP P2P via P2PManager failed: %v\n", err);
			fmt.Printf("Failed to establish UDP P2P: %v\n", err);
		} else {
			// P2PManager内部已打印成功消息 (P2PManager already printed success message internally)
			fmt.Printf("UDP P2P connection with %s initiated. You can now 'msg' or 'tcp'.\n", c.p2pManager.RemoteAddr().String());
		}
	case storage.ConnectDeny: // P2P连接请求被对端拒绝 (P2P connection request denied by peer)
		denyingPeerName := usermsg.Msg; fmt.Printf("Connection request was denied by '%s'. (连接请求被 '%s' 拒绝)\n", denyingPeerName, denyingPeerName);
	case storage.Msg: // 来自服务器的通用消息或重要通知 (General message or important notification from server)
		fmt.Printf("[Server]: %s\n", usermsg.Msg);
		wasPreviouslyUnnamed := (c.clientName == "") // 检查是否是客户端首次被分配名称 (Check if this is the client's first name assignment)

		// 处理服务器分配的名称 (Handle server-assigned name)
		if strings.HasPrefix(usermsg.Msg, "Registered. Your name is: ") {
			newName := strings.TrimPrefix(usermsg.Msg, "Registered. Your name is: ");
			if c.clientName != newName { c.clientName = newName; log.Printf("[INFO] Name assigned by server: %s\n", c.clientName); }
			if wasPreviouslyUnnamed && c.clientName == newName { // 如果是首次分配, 显示欢迎语 (If first assignment, show welcome message)
				fmt.Printf("Welcome, %s! You are connected. Type 'help' for commands.\n", c.clientName);
			}
		} else if strings.HasPrefix(usermsg.Msg, "Rename successful. Your new name is: ") { // 处理重命名成功的通知 (Handle successful rename notification)
			newName := strings.TrimPrefix(usermsg.Msg, "Rename successful. Your new name is: ");
			if c.clientName != newName { c.clientName = newName; log.Printf("[INFO] Name successfully changed to: %s\n", c.clientName); fmt.Printf("Your name is now: %s\n", c.clientName); }
		} else if strings.HasPrefix(usermsg.Msg, "Error: Malformed message format.") { // 服务器报告客户端发送了格式错误的消息 (Server reports client sent a malformed message)
			log.Printf("[WARN] Server reported our last message was malformed.\n");
		}
	default: // 未知类型的服务器消息 (Unknown server message type)
		log.Printf("[WARN] Received unhandled message type %d from server %s: %s\n", usermsg.MsgType, remoteAddr.String(), usermsg.Msg);
	}
	fmt.Print(c.getCurrentPrompt()); // 处理完所有来自服务器的消息后, 重新打印提示符 (After handling all server messages, reprint the prompt)
}

// RunClientMode 是客户端模式的入口函数 (Entry point for client mode)
// 此函数将由 main.go 调用 (This function will be called from main.go)
func RunClientMode(clientPort int, serverAddr string) {
	client := NewClient() // 创建客户端实例 (Create client instance)
	client.Start(clientPort, serverAddr) // 启动客户端 (Start the client)
}
>>>>>>> REPLACE
