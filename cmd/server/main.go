package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go-p2p/internal/config"
	"go-p2p/internal/log"
	"go-p2p/internal/message"
)

// ClientInfo 客户端信息
type ClientInfo struct {
	Name         string       // 客户端名称
	Addr         *net.UDPAddr // 客户端地址
	LastSeen     time.Time    // 最后一次看到的时间
	Connected    bool         // 是否已连接
	ConnectTime  time.Time    // 连接时间
	MessageCount int64        // 消息计数
	LastConnect  time.Time    // 最后连接尝试时间
}

// Server 服务器结构体
type Server struct {
	conn           *net.UDPConn           // UDP连接
	clients        map[string]*ClientInfo // 客户端映射
	addrToName     map[string]string      // 地址到名称的映射
	clientsMutex   sync.RWMutex           // 客户端映射互斥锁
	isRunning      bool                   // 是否正在运行
	stopChan       chan struct{}          // 停止信号通道
	stats          *ServerStats           // 服务器统计信息
	connectLimiter map[string]time.Time   // 连接频率限制
	limiterMutex   sync.RWMutex           // 限制器互斥锁
}

// ServerStats 服务器统计信息
type ServerStats struct {
	TotalConnections int64     // 总连接数
	ActiveClients    int64     // 活跃客户端数
	TotalMessages    int64     // 总消息数
	StartTime        time.Time // 启动时间
}

// NewServer 创建新的服务器实例
func NewServer() *Server {
	return &Server{
		clients:        make(map[string]*ClientInfo),
		addrToName:     make(map[string]string),
		stopChan:       make(chan struct{}),
		connectLimiter: make(map[string]time.Time),
		stats: &ServerStats{
			StartTime: time.Now(),
		},
	}
}

// validateClientName 验证客户端名称
func (s *Server) validateClientName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("client name cannot be empty")
	}
	if len(name) > 32 {
		return fmt.Errorf("client name too long (max 32 characters)")
	}
	// 只允许字母、数字、下划线和连字符
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	if !matched {
		return fmt.Errorf("client name contains invalid characters")
	}
	return nil
}

// checkConnectLimit 检查连接频率限制
func (s *Server) checkConnectLimit(addr string) bool {
	s.limiterMutex.Lock()
	defer s.limiterMutex.Unlock()

	lastConnect, exists := s.connectLimiter[addr]
	if exists && time.Since(lastConnect) < 5*time.Second {
		return false // 连接太频繁
	}
	s.connectLimiter[addr] = time.Now()
	return true
}

// getStats 获取服务器统计信息
func (s *Server) getStats() ServerStats {
	s.clientsMutex.RLock()
	activeClients := int64(len(s.clients))
	s.clientsMutex.RUnlock()

	return ServerStats{
		TotalConnections: atomic.LoadInt64(&s.stats.TotalConnections),
		ActiveClients:    activeClients,
		TotalMessages:    atomic.LoadInt64(&s.stats.TotalMessages),
		StartTime:        s.stats.StartTime,
	}
}

// Start 启动服务器
func (s *Server) Start(port int) error {
	// 创建UDP地址
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	// 监听UDP端口
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %v", port, err)
	}

	s.conn = conn
	s.isRunning = true

	log.Info("Server started on port %d", port)

	// 启动消息处理循环
	go s.messageLoop()

	// 启动客户端清理循环
	go s.cleanupLoop()

	return nil
}

// Stop 停止服务器
func (s *Server) Stop() {
	if !s.isRunning {
		return
	}

	// 发送停止信号
	close(s.stopChan)

	// 关闭连接
	if s.conn != nil {
		s.conn.Close()
	}

	s.isRunning = false
	log.Info("Server stopped")
}

// messageLoop 消息处理循环
func (s *Server) messageLoop() {
	buffer := make([]byte, 8192) // 增加缓冲区大小

	for {
		select {
		case <-s.stopChan:
			return
		default:
			// 设置读取超时
			s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			// 读取消息
			n, addr, err := s.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}
				log.Error("Error reading from UDP: %v", err)
				continue
			}

			// 验证消息大小
			if n > 4096 {
				log.Warn("Received oversized message (%d bytes) from %s", n, addr.String())
				continue
			}

			// 解析消息
			msg, err := message.Deserialize(buffer[:n])
			if err != nil {
				log.Error("Failed to deserialize message from %s: %v", addr.String(), err)
				continue
			}

			// 更新统计信息
			atomic.AddInt64(&s.stats.TotalMessages, 1)

			// 处理消息
			s.handleMessage(msg, addr)
		}
	}
}

// cleanupLoop 客户端清理循环
func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.cleanupExpiredClients()
		}
	}
}

// handleMessage 处理接收到的消息
func (s *Server) handleMessage(msg *message.BaseMessage, addr *net.UDPAddr) {
	switch msg.GetType() {
	case message.Heartbeat:
		// 处理心跳消息
		s.handleHeartbeat(msg.Data, addr)
	case message.Connect:
		// 处理连接消息
		s.handleConnect(msg.Data, addr)
	case message.SearchAll:
		// 处理搜索所有客户端消息
		s.handleSearchAll(addr)
	case message.ConnectTo:
		// 处理连接到其他客户端的请求
		s.handleConnectTo(msg.Data, addr)
	}
}

// handleHeartbeat 处理心跳消息
func (s *Server) handleHeartbeat(data string, addr *net.UDPAddr) {
	// 使用地址映射快速查找客户端
	s.clientsMutex.RLock()
	clientName, exists := s.addrToName[addr.String()]
	s.clientsMutex.RUnlock()

	if !exists {
		log.Warn("Received heartbeat from unknown client: %s", addr.String())
		return
	}

	// 更新客户端最后一次看到的时间
	s.clientsMutex.Lock()
	if client, exists := s.clients[clientName]; exists {
		client.LastSeen = time.Now()
		atomic.AddInt64(&client.MessageCount, 1)
	}
	s.clientsMutex.Unlock()
}

// handleConnect 处理连接消息
func (s *Server) handleConnect(data string, addr *net.UDPAddr) {
	// 检查连接频率限制
	if !s.checkConnectLimit(addr.String()) {
		log.Warn("Connection rate limit exceeded for %s", addr.String())
		return
	}

	// 解析客户端名称
	clientName := strings.TrimSpace(data)

	// 验证客户端名称
	if err := s.validateClientName(clientName); err != nil {
		log.Error("Invalid client name from %s: %v", addr.String(), err)
		s.sendMessage(message.NewConnectDenyMessage(fmt.Sprintf("Invalid name: %v", err)), addr)
		return
	}

	// 检查客户端名称是否已存在
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	for name, client := range s.clients {
		if name == clientName && client.Addr.String() != addr.String() {
			// 名称已被使用，发送拒绝消息
			s.sendMessage(message.NewConnectDenyMessage(fmt.Sprintf("Name '%s' already in use", clientName)), addr)
			log.Warn("Client name '%s' already in use, rejected %s", clientName, addr.String())
			return
		}
	}

	// 更新或添加客户端信息
	client, exists := s.clients[clientName]
	if !exists {
		client = &ClientInfo{
			Name:        clientName,
			Addr:        addr,
			LastSeen:    time.Now(),
			Connected:   true,
			ConnectTime: time.Now(),
			LastConnect: time.Now(),
		}
		s.clients[clientName] = client
		s.addrToName[addr.String()] = clientName
		atomic.AddInt64(&s.stats.TotalConnections, 1)
		log.Info("New client connected: %s (%s)", clientName, addr.String())
	} else {
		// 更新现有客户端信息
		oldAddr := client.Addr.String()
		client.Addr = addr
		client.LastSeen = time.Now()
		client.Connected = true
		client.LastConnect = time.Now()

		// 更新地址映射
		if oldAddr != addr.String() {
			delete(s.addrToName, oldAddr)
			s.addrToName[addr.String()] = clientName
		}

		log.Info("Client reconnected: %s (%s)", clientName, addr.String())
	}

	// 发送连接允许消息
	// 创建包含连接信息的JSON数据
	connectInfo := struct {
		PeerName  string `json:"peer_name"`
		PeerAddr  string `json:"peer_addr"`
		LocalAddr string `json:"local_addr"`
	}{
		PeerName:  clientName,
		PeerAddr:  addr.String(),
		LocalAddr: addr.String(),
	}

	connectData, err := json.Marshal(connectInfo)
	if err != nil {
		log.Error("Failed to marshal connect info: %v", err)
		return
	}

	s.sendMessage(message.NewConnectAllowMessage(string(connectData)), addr)
}

// handleSearchAll 处理搜索所有客户端消息
func (s *Server) handleSearchAll(addr *net.UDPAddr) {
	// 验证请求来源
	s.clientsMutex.RLock()
	requesterName, exists := s.addrToName[addr.String()]
	if !exists {
		s.clientsMutex.RUnlock()
		log.Warn("Search request from unknown client: %s", addr.String())
		return
	}

	// 构建客户端列表（排除请求者自己）
	clientList := make(map[string]string)
	for name, client := range s.clients {
		if client.Connected && name != requesterName {
			clientList[name] = client.Addr.String()
		}
	}
	s.clientsMutex.RUnlock()

	// 序列化客户端列表
	data, err := json.Marshal(clientList)
	if err != nil {
		log.Error("Failed to serialize client list: %v", err)
		return
	}

	// 发送客户端列表
	s.sendMessage(message.NewMessage(message.SearchAll, string(data)), addr)
	log.Info("Sent client list to %s (%d clients)", requesterName, len(clientList))
}

// handleConnectTo 处理连接到其他客户端的请求
func (s *Server) handleConnectTo(data string, addr *net.UDPAddr) {
	// 解析目标客户端名称
	targetName := strings.TrimSpace(data)
	if targetName == "" {
		log.Error("Received connect-to message with empty target name from %s", addr.String())
		return
	}

	// 查找源客户端名称
	s.clientsMutex.RLock()
	sourceClientName, sourceExists := s.addrToName[addr.String()]
	if !sourceExists {
		s.clientsMutex.RUnlock()
		log.Error("Unknown client requesting connection: %s", addr.String())
		return
	}

	// 查找目标客户端
	targetClient, targetExists := s.clients[targetName]
	s.clientsMutex.RUnlock()

	if !targetExists || !targetClient.Connected {
		// 目标客户端不存在或未连接，发送拒绝消息
		s.sendMessage(message.NewConnectDenyMessage(fmt.Sprintf("Target '%s' not found or offline", targetName)), addr)
		log.Warn("Target client '%s' not found or not connected, requested by %s", targetName, sourceClientName)
		return
	}

	// 防止自己连接自己
	if sourceClientName == targetName {
		s.sendMessage(message.NewConnectDenyMessage("Cannot connect to yourself"), addr)
		log.Warn("Client %s attempted to connect to itself", sourceClientName)
		return
	}

	// 向源客户端发送连接允许消息
	// 创建包含连接信息的JSON数据
	connectInfo := struct {
		PeerName  string `json:"peer_name"`
		PeerAddr  string `json:"peer_addr"`
		LocalAddr string `json:"local_addr"`
	}{
		PeerName:  targetName,
		PeerAddr:  targetClient.Addr.String(),
		LocalAddr: addr.String(),
	}

	connectData, err := json.Marshal(connectInfo)
	if err != nil {
		log.Error("Failed to marshal connect info: %v", err)
		return
	}

	s.sendMessage(message.NewConnectAllowMessage(string(connectData)), addr)

	// 向目标客户端发送连接请求消息
	s.sendMessage(message.NewConnectToMessage(sourceClientName), targetClient.Addr)

	log.Info("Connection established between %s and %s", sourceClientName, targetName)
}

// cleanupExpiredClients 清理过期的客户端
func (s *Server) cleanupExpiredClients() {
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	expireTime := time.Now().Add(-5 * time.Minute)
	expiredClients := make([]string, 0)

	for name, client := range s.clients {
		if client.LastSeen.Before(expireTime) {
			// 清理地址映射
			delete(s.addrToName, client.Addr.String())
			delete(s.clients, name)
			expiredClients = append(expiredClients, name)
		}
	}

	if len(expiredClients) > 0 {
		log.Info("Cleaned up %d expired clients: %v", len(expiredClients), expiredClients)
	}

	// 清理连接限制器中的过期条目
	s.limiterMutex.Lock()
	limiterExpireTime := time.Now().Add(-1 * time.Hour)
	for addr, lastTime := range s.connectLimiter {
		if lastTime.Before(limiterExpireTime) {
			delete(s.connectLimiter, addr)
		}
	}
	s.limiterMutex.Unlock()
}

// sendMessage 发送消息到指定地址
func (s *Server) sendMessage(msg message.Message, addr *net.UDPAddr) {
	data, err := msg.Serialize()
	if err != nil {
		log.Error("Failed to serialize message: %v", err)
		return
	}

	// 验证消息大小
	if len(data) > 4096 {
		log.Error("Message too large (%d bytes), cannot send to %s", len(data), addr.String())
		return
	}

	_, err = s.conn.WriteToUDP(data, addr)
	if err != nil {
		log.Error("Failed to send message to %s: %v", addr.String(), err)
	}
}

func main() {
	// 初始化日志
	log.SetLevel(log.INFO)
	log.SetPrefix("[Server] ")
	log.Info("Starting P2P Discovery Server...")

	// 加载配置
	cfg := config.GetConfig()
	err := config.LoadFromFile("config.json")
	if err != nil {
		log.Warn("Failed to load config file, using defaults: %v", err)
	}

	// 创建服务器
	server := NewServer()

	// 启动服务器
	err = server.Start(cfg.Server.Port)
	if err != nil {
		log.Fatal("Failed to start server: %v", err)
	}

	// 启动统计信息输出
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-server.stopChan:
				return
			case <-ticker.C:
				stats := server.getStats()
				uptime := time.Since(stats.StartTime)
				log.Info("Server Stats - Uptime: %v, Active Clients: %d, Total Connections: %d, Total Messages: %d",
					uptime.Round(time.Second), stats.ActiveClients, stats.TotalConnections, stats.TotalMessages)
			}
		}
	}()

	log.Info("P2P Discovery Server started successfully on port %d", cfg.Server.Port)

	// 处理系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 停止服务器
	log.Info("Shutting down server...")
	stats := server.getStats()
	uptime := time.Since(stats.StartTime)
	log.Info("Final Stats - Uptime: %v, Total Connections: %d, Total Messages: %d",
		uptime.Round(time.Second), stats.TotalConnections, stats.TotalMessages)
	server.Stop()
	log.Info("Server stopped gracefully")
}
