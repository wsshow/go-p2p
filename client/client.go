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
	"time"
)

var serverConn *net.UDPConn
var p2pConnUDP *net.UDPConn
var p2pConnTCP *net.TCPConn

var localUDPAddr *net.UDPAddr
var peerP2PUDPAddr *net.UDPAddr
var localP2PUDPAddrForPeer *net.UDPAddr

var clientName string
// var promptPrefix = "> " // Replaced by getCurrentPrompt

type Command struct {
	Description string
	Usage       string
	Handler     func(args []string) error
}

var commands map[string]Command

func initCommands() {
	commands = map[string]Command{
		"all": {
			Description: "List all connected clients. (列出所有已连接客户端)",
			Usage:       "all",
			Handler:     handleListAll,
		},
		"connect": {
			Description: "Request to connect to another client by name. (请求连接到指定名称的客户端)",
			Usage:       "connect <client_name>",
			Handler:     handleConnectTo,
		},
		"allow": {
			Description: "Allow a connection request from another client. (允许来自其他客户端的连接请求)",
			Usage:       "allow <client_name>",
			Handler:     handleAllowConnect,
		},
		"deny": {
			Description: "Deny a connection request from another client. (拒绝来自其他客户端的连接请求)",
			Usage:       "deny <client_name>",
			Handler:     handleDenyConnect,
		},
		"msg": {
			Description: "Send a message to the connected peer. (发送消息给已连接的对端)",
			Usage:       "msg <message_content>",
			Handler:     handleSendMessage,
		},
		"rename": {
			Description: "Request to change your client name on the server. (请求在服务器上更改您的客户端名称)",
			Usage:       "rename <new_name>",
			Handler:     handleRename,
		},
		"tcp": {
			Description: "Switch the current P2P UDP connection to TCP. (将当前的P2P UDP连接切换到TCP)",
			Usage:       "tcp",
			Handler:     handleChangeToTCP,
		},
		"help": {
			Description: "Show this help message. (显示此帮助信息)",
			Usage:       "help",
			Handler:     handleHelp,
		},
		"exit": {
			Description: "Close connections and exit the client. (关闭连接并退出客户端)",
			Usage:       "exit",
			Handler:     handleExit,
		},
	}
}

func handleHelp(args []string) error {
	fmt.Println("\nAvailable commands (可用命令):")
	for name, cmd := range commands {
		fmt.Printf("  %s: %s\n", name, cmd.Description)
		fmt.Printf("    Usage (用法): %s\n", cmd.Usage)
	}
	fmt.Println()
	return nil
}

func handleListAll(args []string) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	return sendToServer(storage.UserMsg{MsgType: storage.SearchAll})
}

func handleConnectTo(args []string) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage (用法): connect <client_name>")
	}
	targetName := args[0]
	if targetName == clientName {
		return fmt.Errorf("cannot connect to yourself (不能连接到自己)")
	}
	fmt.Printf("Requesting connection to %s...\n", targetName)
	return sendToServer(storage.UserMsg{MsgType: storage.ConnectTo, Msg: targetName})
}

func handleAllowConnect(args []string) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage (用法): allow <client_name_who_requested>")
	}
	requesterName := args[0]
	err := sendToServer(storage.UserMsg{MsgType: storage.ConnectAllow, Msg: requesterName})
	if err != nil {
		return fmt.Errorf("failed to send allow command: %w", err)
	}
	fmt.Printf("Sent allow confirmation for %s to server.\n", requesterName)
	return nil
}

func handleDenyConnect(args []string) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage (用法): deny <client_name_who_requested>")
	}
	requesterName := args[0]
	fmt.Printf("Sending deny for %s to server.\n", requesterName)
	return sendToServer(storage.UserMsg{MsgType: storage.ConnectDeny, Msg: requesterName})
}

func handleSendMessage(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage (用法): msg <message_content>")
	}
	message := strings.Join(args, " ")
	userMsg := storage.UserMsg{MsgType: storage.Msg, Msg: message}

	if p2pConnTCP != nil {
		fmt.Printf("[Me -> TCP %s]: %s\n", p2pConnTCP.RemoteAddr().String(), message)
		return sendP2PMessageTCP(userMsg)
	}
	if p2pConnUDP != nil {
		remoteDispAddr := "peer"
		if p2pConnUDP.RemoteAddr()!=nil { remoteDispAddr = p2pConnUDP.RemoteAddr().String() } else if peerP2PUDPAddr != nil { remoteDispAddr = peerP2PUDPAddr.String()}
		fmt.Printf("[Me -> UDP %s]: %s\n", remoteDispAddr, message)
		return sendP2PMessageUDP(userMsg)
	}
	return fmt.Errorf("not connected to any peer (no active P2P UDP or TCP connection) (未连接到任何对端)")
}

func handleRename(args []string) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage (用法): rename <new_name>")
	}
	newName := args[0]
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("new name cannot be empty (新名称不能为空)")
	}
	fmt.Printf("Requesting rename to '%s'...\n", newName)
	return sendToServer(storage.UserMsg{MsgType: storage.Rename, Msg: newName})
}

func handleChangeToTCP(args []string) error {
	if p2pConnUDP == nil {
		return fmt.Errorf("no active UDP P2P connection to switch to TCP (没有活动的UDP P2P连接可切换)")
	}
	if peerP2PUDPAddr == nil || localP2PUDPAddrForPeer == nil {
		return fmt.Errorf("P2P UDP address information incomplete, cannot switch to TCP. Peer: %v, LocalForPeer: %v (P2P UDP地址信息不完整)", peerP2PUDPAddr, localP2PUDPAddrForPeer)
	}

	log.Println("[INFO] Initiating switch to TCP with peer...")
	err := sendP2PMessageUDP(storage.UserMsg{MsgType: storage.ChangeToTCP})
	if err != nil {
		return fmt.Errorf("failed to send ChangeToTCP request to peer: %v (发送ChangeToTCP请求失败)", err)
	}

	currentLocalP2PUDPAddr := localP2PUDPAddrForPeer
	currentPeerP2PUDPAddr := peerP2PUDPAddr

	log.Println("[INFO] Closing current UDP P2P connection for TCP switch (as initiator).")
	if p2pConnUDP != nil {
		p2pConnUDP.Close()
		p2pConnUDP = nil
	}

	fmt.Print(getCurrentPrompt())


	time.Sleep(1 * time.Second)

	localTCPAddrToDialFrom := &net.TCPAddr{IP: currentLocalP2PUDPAddr.IP, Port: currentLocalP2PUDPAddr.Port}
	peerTCPAddrToDialTo := &net.TCPAddr{IP: currentPeerP2PUDPAddr.IP, Port: currentPeerP2PUDPAddr.Port}

	log.Printf("[INFO] Attempting TCP connection: dialing from local %s to remote %s\n", localTCPAddrToDialFrom.String(), peerTCPAddrToDialTo.String())

	var tempTCPConn *net.TCPConn
	dialer := net.Dialer{Timeout: 7 * time.Second, LocalAddr: localTCPAddrToDialFrom}
	tempTCPConn, err = dialer.Dial("tcp4", peerTCPAddrToDialTo.String())

	if err != nil {
		log.Printf("[WARN] TCP Dial to %s failed: %v. This client (initiator of 'tcp' command) will now try to listen on %s...\n", peerTCPAddrToDialTo.String(), err, localTCPAddrToDialFrom.String())
		listener, listenErr := net.ListenTCP("tcp4", localTCPAddrToDialFrom)
		if listenErr != nil {
			return fmt.Errorf("failed to dial and also failed to listen on %s for TCP: Dial error: %v; Listen error: %v (TCP拨号和监听均失败)", localTCPAddrToDialFrom.String(), err, listenErr)
		}
		defer listener.Close()
		log.Printf("[INFO] Listening for TCP connection from %s on %s for 7 seconds...\n", peerTCPAddrToDialTo.String(), localTCPAddrToDialFrom.String())
		listener.SetDeadline(time.Now().Add(7 * time.Second))
		tempTCPConn, err = listener.AcceptTCP()
		if err != nil {
			return fmt.Errorf("failed to accept TCP connection after listening: %v. Original dial error was: %v (接受TCP连接失败)", err, listenErr)
		}
		log.Println("[INFO] Accepted TCP connection successfully after initial dial failed.")
	} else {
		log.Printf("[INFO] Successfully dialed TCP to %s.\n", peerTCPAddrToDialTo.String())
	}

	p2pConnTCP = tempTCPConn
	fmt.Printf("\nSuccessfully switched to TCP with %s.\n%s", p2pConnTCP.RemoteAddr().String(), getCurrentPrompt())
	go receiveP2PMessagesTCP()
	return nil
}


func handleExit(args []string) error {
	log.Println("[INFO] Exiting client application...")
	if serverConn != nil {
		serverConn.Close()
		serverConn = nil
	}
	if p2pConnUDP != nil {
		p2pConnUDP.Close()
		p2pConnUDP = nil
	}
	if p2pConnTCP != nil {
		p2pConnTCP.Close()
		p2pConnTCP = nil
	}
	fmt.Println("Connections closed. Bye!")
	os.Exit(0)
	return nil
}

func Run(clientPort int, serverAddr string) {
	initCommands()
	log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
	log.SetPrefix("[go-p2p-client] ")

	saddr, err := net.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		log.Fatalf("[FATAL] Error resolving server address '%s': %v", serverAddr, err)
	}

	localUDPAddr = &net.UDPAddr{IP: net.IPv4zero, Port: clientPort}

	conn, err := net.DialUDP("udp4", localUDPAddr, saddr)
	if err != nil {
		log.Fatalf("[FATAL] Failed to connect to server %s from local port %d: %v", serverAddr, clientPort, err)
	}
	serverConn = conn
    if actualLocalAddr, ok := serverConn.LocalAddr().(*net.UDPAddr); ok {
        localUDPAddr = actualLocalAddr
    }

	log.Printf("[INFO] Connection attempt to server at %s from local UDP address: %s\n", serverAddr, serverConn.LocalAddr().String())

	err = sendToServer(storage.UserMsg{MsgType: storage.Connect})
	if err != nil {
		log.Fatalf("[FATAL] Failed to send connect message to server: %v", err)
	}
	fmt.Println("Sent registration request to server. Waiting for name assignment...")

	go receiveServerMessages()
	go processUserCommands()

	select {}
}

func sendToServer(msg storage.UserMsg) error {
	if serverConn == nil {
		return fmt.Errorf("not connected to server (未连接到服务器)")
	}
	bs, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal server message %+v: %v\n", msg, err)
		return fmt.Errorf("failed to marshal server message: %v (序列化服务器消息失败)", err)
	}
	_, err = serverConn.Write(bs)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "network is unreachable") {
			log.Printf("[ERROR] Error sending to server: Connection refused or network unreachable. Server might be down.\n")
		} else {
			log.Printf("[ERROR] Failed to send message to server: %v\n", err)
		}
		return fmt.Errorf("failed to send message to server: %v (发送消息至服务器失败)", err)
	}
	return nil
}

func sendP2PMessageUDP(msg storage.UserMsg) error {
	if p2pConnUDP == nil {
		return fmt.Errorf("no active UDP P2P connection (无活动的UDP P2P连接)")
	}
	bs, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal P2P UDP message %+v: %v\n", msg, err)
		return fmt.Errorf("failed to marshal P2P UDP message: %v (序列化P2P UDP消息失败)", err)
	}
	_, err = p2pConnUDP.Write(bs)
	if err != nil {
		log.Printf("[ERROR] Failed to send P2P UDP message to %s: %v\n", p2pConnUDP.RemoteAddr().String(), err)
		return fmt.Errorf("failed to send P2P UDP message: %v (发送P2P UDP消息失败)", err)
	}
	return nil
}

func sendP2PMessageTCP(msg storage.UserMsg) error {
	if p2pConnTCP == nil {
		return fmt.Errorf("no active TCP P2P connection (无活动的TCP P2P连接)")
	}
	bs, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal P2P TCP message %+v: %v\n", msg, err)
		return fmt.Errorf("failed to marshal P2P TCP message: %v (序列化P2P TCP消息失败)", err)
	}
	finalMsg := append(bs, '\n')
	_, err = p2pConnTCP.Write(finalMsg)
	if err != nil {
		log.Printf("[ERROR] Failed to send P2P TCP message to %s: %v\n", p2pConnTCP.RemoteAddr().String(), err)
		return fmt.Errorf("failed to send P2P TCP message: %v (发送P2P TCP消息失败)", err)
	}
	return nil
}

func getCurrentPrompt() string {
	namePart := ""
	if clientName != "" {
		namePart = fmt.Sprintf("[%s]", clientName)
	}

	connPart := ""
	if p2pConnTCP != nil && p2pConnTCP.RemoteAddr() != nil {
		connPart = fmt.Sprintf("[TCP %s]", p2pConnTCP.RemoteAddr().String())
	} else if p2pConnUDP != nil && p2pConnUDP.RemoteAddr() != nil {
		connPart = fmt.Sprintf("[UDP %s]", p2pConnUDP.RemoteAddr().String())
	}

	if namePart == "" && connPart == "" {
		return "> "
	}
	return fmt.Sprintf("%s%s> ", namePart, connPart)
}


func processUserCommands() {
	reader := bufio.NewReader(os.Stdin)
	time.Sleep(time.Millisecond * 100) // Reduced sleep, welcome message handles initial prompt better

	for {
		fmt.Print(getCurrentPrompt())

		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				log.Println("[INFO] EOF detected from stdin, initiating exit.")
				handleExit(nil)
				return
			}
			log.Printf("[WARN] Error reading input from stdin: %v\n", err)
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmdName := strings.ToLower(parts[0])
		args := parts[1:]

		if cmd, exists := commands[cmdName]; exists {
			err := cmd.Handler(args)
			if err != nil {
				fmt.Printf("Error (错误): %v\n", err)
			}
		} else {
			fmt.Printf("Unknown command (未知命令): %s. Type 'help' for a list of commands (输入 'help' 查看命令列表).\n", cmdName)
		}
	}
}

func receiveServerMessages() {
	b := make([]byte, 2048)
	var usermsg storage.UserMsg
	for {
		if serverConn == nil {
			log.Println("[INFO] Server connection is nil in receiveServerMessages. Terminating goroutine.")
			return
		}
		n, remoteAddr, err := serverConn.ReadFromUDP(b)
		if err != nil {
			if serverConn == nil || strings.Contains(err.Error(), "use of closed network connection") {
				fmt.Printf("\n[INFO] Connection to server has been closed or lost.\n%s", getCurrentPrompt())
				if serverConn != nil { serverConn.Close(); serverConn = nil; }
				return
			}
			log.Printf("[WARN] Error reading from server UDP %s: %v\n", remoteAddr, err)
			time.Sleep(1 * time.Second)
			continue
		}

		err = json.Unmarshal(b[:n], &usermsg)
		if err != nil {
			log.Printf("[WARN] Error unmarshalling message from server %s: %v. Data: %s\n", remoteAddr, err, string(b[:n]))
			continue
		}
		processIncomingMessageFromServer(usermsg, remoteAddr)
	}
}

func processIncomingMessageFromServer(usermsg storage.UserMsg, remoteAddr net.Addr) {
	// Capture prompt state *before* printing any message from server,
	// so that if the message itself triggers a state change (like name assignment),
	// the prompt printed *after* the message reflects the new state.
	// However, for the welcome message, we want the prompt *after* the welcome.
	// This is a bit tricky. Let's get it for general use first.
	// currentPromptText := getCurrentPrompt() // Not used if logic below prints its own prompt

	switch usermsg.MsgType {
	case storage.Heartbeat:
		err := sendToServer(storage.UserMsg{MsgType: storage.Heartbeat, Msg: usermsg.Msg})
		if err != nil {
			log.Printf("[ERROR] Failed to send heartbeat response to server: %v\n", err)
		}

	case storage.ConnectTo:
		peerNameRequesting := usermsg.Msg
		fmt.Printf("\nIncoming connection request from '%s'. (收到来自 '%s' 的连接请求)\nTo accept (接受): allow %s\nTo deny (拒绝): deny %s\n%s",
			peerNameRequesting, peerNameRequesting, peerNameRequesting, peerNameRequesting, getCurrentPrompt())

	case storage.ConnectAllow:
		parts := strings.Split(usermsg.Msg, ",")
		if len(parts) != 2 {
			log.Printf("[ERROR] Invalid ConnectAllow message from server: %s. Expected 2 parts, got %d.\n", usermsg.Msg, len(parts))
			return
		}
		peerAddrStr := parts[0]
		myAddrStrAtServer := parts[1]

		var err error
		tempPeerP2PUDPAddr, err := net.ResolveUDPAddr("udp4", peerAddrStr)
		if err != nil {
			log.Printf("[ERROR] Error resolving peer UDP address '%s' for P2P: %v\n", peerAddrStr, err)
			return
		}
		peerP2PUDPAddr = tempPeerP2PUDPAddr

		tempLocalP2PUDPAddrForPeer, err := net.ResolveUDPAddr("udp4", myAddrStrAtServer)
        if err != nil {
            log.Printf("[ERROR] Error resolving local P2P UDP address '%s' (our external addr from server): %v\n", myAddrStrAtServer, err)
            return
        }
		localP2PUDPAddrForPeer = tempLocalP2PUDPAddrForPeer

		fmt.Printf("\nServer authorized P2P connection. (服务器授权P2P连接)\nPeer's address for P2P (对端P2P地址): %s\nYour address for P2P (as seen by peer) (您的P2P地址/对端视角): %s\n", peerP2PUDPAddr.String(), localP2PUDPAddrForPeer.String())
		log.Println("[INFO] Attempting UDP P2P connection (hole punching)...")

		if p2pConnUDP != nil {
			log.Println("[INFO] Closing existing UDP P2P connection before establishing new one.")
			p2pConnUDP.Close()
			p2pConnUDP = nil
		}

		dialConn, err := net.DialUDP("udp4", localP2PUDPAddrForPeer, peerP2PUDPAddr)
		if err != nil {
			log.Printf("[WARN] Failed to dial UDP to peer %s using specific local %s: %v. Trying general dial (nil local addr).\n", peerP2PUDPAddr.String(), localP2PUDPAddrForPeer.String(), err)
			dialConn, err = net.DialUDP("udp4", nil, peerP2PUDPAddr)
			if err != nil {
				log.Printf("[ERROR] General UDP dial to peer %s also failed: %v\n", peerP2PUDPAddr.String(), err)
				fmt.Printf("\nFailed to establish UDP P2P connection with %s. (建立UDP P2P连接失败)\n%s", peerP2PUDPAddr.String(), getCurrentPrompt())
				return
			}
		}
		p2pConnUDP = dialConn

		log.Printf("[INFO] UDP P2P connection attempt made to %s (using local: %s).\n",
			p2pConnUDP.RemoteAddr().String(), p2pConnUDP.LocalAddr().String())
		fmt.Printf("\nUDP P2P connection established with %s. (已与 %s 建立UDP P2P连接)\n", p2pConnUDP.RemoteAddr().String(), p2pConnUDP.RemoteAddr().String())

		go func() {
			for i := 0; i < 3; i++ {
				if p2pConnUDP == nil { break }
				msgContent := fmt.Sprintf("(UDP P2P Hello from %s - Attempt %d)", clientName, i+1)
				sendP2PMessageUDP(storage.UserMsg{MsgType: storage.Msg, Msg: msgContent})
				time.Sleep(300 * time.Millisecond)
			}
		}()
		go receiveP2PMessagesUDP()

		fmt.Printf("You can now try 'msg <text>' to %s or 'tcp' to switch protocol.\n%s", peerP2PUDPAddr.String(), getCurrentPrompt())


	case storage.ConnectDeny:
		denyingPeerName := usermsg.Msg
		fmt.Printf("\nConnection request was denied by '%s'. (连接请求被 '%s' 拒绝)\n%s", denyingPeerName, denyingPeerName, getCurrentPrompt())

	case storage.Msg:
		// Handle name assignment and other server messages
		wasPreviouslyUnnamed := (clientName == "")

		if strings.HasPrefix(usermsg.Msg, "Registered. Your name is: ") {
			newName := strings.TrimPrefix(usermsg.Msg, "Registered. Your name is: ")
			if clientName != newName {
				clientName = newName
				log.Printf("[INFO] Name assigned by server: %s\n", clientName)
				// Welcome message only if it's the *first* name assignment
				if wasPreviouslyUnnamed {
					fmt.Printf("\nWelcome, %s! You are connected to the server. Type 'help' for commands.\n", clientName)
				} else { // Just a regular server message if name was already set (e.g. a re-confirmation)
					fmt.Printf("\n[Server]: %s\n", usermsg.Msg)
				}
			} else { // Name is the same, just a server message
				fmt.Printf("\n[Server]: %s\n", usermsg.Msg)
			}
		} else if strings.HasPrefix(usermsg.Msg, "Rename successful. Your new name is: ") {
			newName := strings.TrimPrefix(usermsg.Msg, "Rename successful. Your new name is: ")
			if clientName != newName {
				clientName = newName
				log.Printf("[INFO] Name successfully changed to: %s\n", clientName)
				fmt.Printf("\nYour name is now: %s\n", clientName)
			} else { // Name unchanged, still print server message
                 fmt.Printf("\n[Server]: %s\n", usermsg.Msg)
            }
		} else if strings.HasPrefix(usermsg.Msg, "Error: Malformed message format.") {
            log.Printf("[WARN] Server reported our last message was malformed.\n")
			fmt.Printf("\n[Server]: %s\n", usermsg.Msg)
        } else { // Other generic server messages
			fmt.Printf("\n[Server]: %s\n", usermsg.Msg)
		}
		fmt.Print(getCurrentPrompt()) // Ensure prompt is displayed after any server message

	default:
		log.Printf("[WARN] Received unhandled message type %d from server %s: %s\n", usermsg.MsgType, remoteAddr.String(), usermsg.Msg)
		fmt.Print(getCurrentPrompt())
	}
}

func processIncomingMessageFromPeer(usermsg storage.UserMsg, remoteAddr net.Addr, connType string) {
	currentPromptText := getCurrentPrompt()

	switch usermsg.MsgType {
	case storage.Msg:
		fmt.Printf("\n[%s %s]: %s\n%s", connType, remoteAddr.String(), usermsg.Msg, currentPromptText)
		if connType == "UDP" && strings.Contains(usermsg.Msg, "(UDP P2P Hello from") {
			log.Printf("[INFO] Received UDP P2P Hello from %s. Connection seems active.\n", remoteAddr.String())
		}

	case storage.ChangeToTCP:
		if connType != "UDP" || p2pConnUDP == nil {
			log.Printf("[WARN] Received ChangeToTCP request but not on an active UDP P2P connection (%s). Ignoring.\n", connType)
			return
		}
		log.Printf("[INFO] Peer %s requests to switch to TCP. This client (receiver) will close UDP and listen for TCP.\n", remoteAddr.String())

		currentLocalP2PUDPAddr := localP2PUDPAddrForPeer
		currentPeerP2PUDPAddr := peerP2PUDPAddr

		log.Println("[INFO] Closing current UDP P2P connection for TCP switch (as receiver).")
		if p2pConnUDP != nil {
			p2pConnUDP.Close()
			p2pConnUDP = nil
		}

		fmt.Print(getCurrentPrompt())

		time.Sleep(500 * time.Millisecond)

		localTCPAddrToListenOn := &net.TCPAddr{IP: currentLocalP2PUDPAddr.IP, Port: currentLocalP2PUDPAddr.Port}

		log.Printf("[INFO] This client (receiver of ChangeToTCP) will listen on %s for incoming TCP connection.\n", localTCPAddrToListenOn.String())
		listener, err := net.ListenTCP("tcp4", localTCPAddrToListenOn)
		if err != nil {
			log.Printf("[ERROR] Failed to listen for TCP connection on %s: %v\n", localTCPAddrToListenOn.String(), err)
			fmt.Printf("\nFailed to set up TCP listener. P2P connection may be lost. (建立TCP监听失败)\n%s", currentPromptText)
			return
		}
		defer listener.Close()

		log.Printf("[INFO] Listening for incoming TCP connection from peer (expected from %s) for 10 seconds...\n", currentPeerP2PUDPAddr.String())
		listener.SetDeadline(time.Now().Add(10 * time.Second))

		tempTCPConn, err := listener.AcceptTCP()
		if err != nil {
			log.Printf("[ERROR] Failed to accept TCP connection on %s: %v\n", localTCPAddrToListenOn.String(), err)
			fmt.Printf("\nFailed to accept incoming TCP connection. P2P connection may be lost. (接受TCP连接失败)\n%s", currentPromptText)
			return
		}

		p2pConnTCP = tempTCPConn
		log.Printf("[INFO] Successfully accepted TCP connection from %s.\n", p2pConnTCP.RemoteAddr().String())
		fmt.Printf("\nSuccessfully switched to TCP with %s.\n%s", p2pConnTCP.RemoteAddr().String(), getCurrentPrompt())
		go receiveP2PMessagesTCP()

	default:
		log.Printf("[WARN] Received unhandled message type %d over %s P2P from %s: %s\n", usermsg.MsgType, connType, remoteAddr.String(), usermsg.Msg)
		fmt.Print(currentPromptText)
	}
}


func receiveP2PMessagesUDP() {
	b := make([]byte, 2048)
	var usermsg storage.UserMsg
	for {
		if p2pConnUDP == nil {
			log.Println("[INFO] p2pConnUDP is nil in receiveP2PMessagesUDP. Terminating goroutine.")
			return
		}

		n, remoteAddr, err := p2pConnUDP.ReadFromUDP(b)
		if err != nil {
			if p2pConnUDP == nil || strings.Contains(err.Error(), "use of closed network connection") {
				log.Println("[INFO] UDP P2P connection was closed.")
				fmt.Printf("\nUDP P2P connection was closed.\n%s", getCurrentPrompt())
				if p2pConnUDP != nil { p2pConnUDP.Close(); p2pConnUDP = nil; }
				return
			}
			log.Printf("[ERROR] Error reading from P2P UDP %s: %v\n", p2pConnUDP.RemoteAddr(), err)
			fmt.Printf("\nUDP P2P connection lost due to error. (UDP P2P连接因错误丢失)\n%s", getCurrentPrompt())
			if p2pConnUDP != nil { p2pConnUDP.Close(); p2pConnUDP = nil; }
			return
		}
		err = json.Unmarshal(b[:n], &usermsg)
		if err != nil {
			log.Printf("[WARN] Error unmarshalling P2P UDP message from %s: %v. Data: %s\n", remoteAddr, err, string(b[:n]))
			continue
		}
		processIncomingMessageFromPeer(usermsg, remoteAddr, "UDP")
	}
}

func receiveP2PMessagesTCP() {
	if p2pConnTCP == nil {
		log.Println("[ERROR] receiveP2PMessagesTCP started with nil p2pConnTCP.")
		return
	}
	reader := bufio.NewReader(p2pConnTCP)
	for {
		if p2pConnTCP == nil {
			log.Println("[INFO] p2pConnTCP is nil in receiveP2PMessagesTCP. Terminating goroutine.")
			return
		}

		jsonBytes, err := reader.ReadBytes('\n')
		if err != nil {
			if p2pConnTCP == nil || err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				log.Println("[INFO] TCP P2P connection was closed.")
				fmt.Printf("\nTCP P2P connection was closed.\n%s", getCurrentPrompt())
				if p2pConnTCP != nil { p2pConnTCP.Close(); p2pConnTCP = nil; }
				return
			}
			log.Printf("[ERROR] Error reading from P2P TCP stream %s: %v\n", p2pConnTCP.RemoteAddr(), err)
			fmt.Printf("\nTCP P2P connection lost due to error. (TCP P2P连接因错误丢失)\n%s", getCurrentPrompt())
			if p2pConnTCP != nil { p2pConnTCP.Close(); p2pConnTCP = nil; }
			return
		}

		var usermsg storage.UserMsg
		err = json.Unmarshal(jsonBytes, &usermsg)
		if err != nil {
			log.Printf("[WARN] Error unmarshalling P2P TCP message from %s: %v. Data: %s\n", p2pConnTCP.RemoteAddr(), err, string(jsonBytes))
			continue
		}
		processIncomingMessageFromPeer(usermsg, p2pConnTCP.RemoteAddr(), "TCP")
	}
}
