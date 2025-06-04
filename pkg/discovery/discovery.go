package discovery

import (
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"go-p2p/internal/log"
	"go-p2p/internal/message"
)

// PeerInfo 对等节点信息
type PeerInfo struct {
	Name        string       // 节点名称
	Address     *net.UDPAddr // 节点地址
	LastSeen    time.Time    // 最后一次看到的时间
	IsConnected bool         // 是否已连接
}

// PeerDiscoveryEvent 对等节点发现事件类型
type PeerDiscoveryEvent int

const (
	// PeerAdded 节点添加事件
	PeerAdded PeerDiscoveryEvent = iota
	// PeerRemoved 节点移除事件
	PeerRemoved
	// PeerUpdated 节点更新事件
	PeerUpdated
)

// PeerEventData 对等节点事件数据
type PeerEventData struct {
	EventType PeerDiscoveryEvent
	Peer      PeerInfo
}

// PeerEventListener 对等节点事件监听器
type PeerEventListener interface {
	OnPeerEvent(event PeerEventData)
}

// PeerDiscovery 对等节点发现接口
type PeerDiscovery interface {
	// Start 启动发现服务
	Start() error
	// Stop 停止发现服务
	Stop() error
	// GetPeers 获取所有对等节点
	GetPeers() []PeerInfo
	// GetPeer 获取指定对等节点
	GetPeer(name string) (PeerInfo, bool)
	// AddListener 添加事件监听器
	AddListener(listener PeerEventListener)
	// RemoveListener 移除事件监听器
	RemoveListener(listener PeerEventListener)
	// UpdatePeerStatus 更新对等节点状态
	UpdatePeerStatus(name string, isConnected bool) error
}

// ServerBasedDiscovery 基于服务器的节点发现实现
type ServerBasedDiscovery struct {
	serverAddr *net.UDPAddr        // 服务器地址
	conn       *net.UDPConn        // UDP连接
	peers      map[string]PeerInfo // 对等节点映射
	listeners  []PeerEventListener // 事件监听器
	isRunning  bool                // 是否正在运行
	stopChan   chan struct{}       // 停止信号通道
	mutex      sync.RWMutex        // 互斥锁
	clientName string              // 客户端名称
}

// NewServerBasedDiscovery 创建基于服务器的节点发现
func NewServerBasedDiscovery(serverAddr *net.UDPAddr, clientName string) *ServerBasedDiscovery {
	return &ServerBasedDiscovery{
		serverAddr: serverAddr,
		peers:      make(map[string]PeerInfo),
		listeners:  make([]PeerEventListener, 0),
		stopChan:   make(chan struct{}),
		clientName: clientName,
	}
}

// Start 启动发现服务
func (d *ServerBasedDiscovery) Start() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.isRunning {
		return errors.New("discovery service is already running")
	}

	log.Info("Creating UDP connection to server...")
	// 创建UDP连接
	conn, err := net.DialUDP("udp", nil, d.serverAddr)
	if err != nil {
		log.Error("Failed to create UDP connection: %v", err)
		return err
	}
	d.conn = conn
	log.Info("UDP connection created successfully")

	// 标记为运行中
	d.isRunning = true
	d.stopChan = make(chan struct{})

	log.Info("Starting receive loop...")
	// 启动接收协程
	go d.receiveLoop()

	log.Info("Starting heartbeat loop...")
	// 启动心跳协程
	go d.heartbeatLoop()

	log.Info("Sending initial connect message...")
	// 发送初始连接消息 - 直接调用而不通过sendMessage避免死锁
	msg := message.NewConnectMessage()
	data, err := msg.Serialize()
	if err != nil {
		log.Error("Failed to serialize connect message: %v", err)
		d.Stop()
		return err
	}
	_, err = conn.Write(data)
	if err != nil {
		log.Error("Failed to send connect message: %v", err)
		d.Stop()
		return err
	}
	log.Info("Initial connect message sent successfully")

	log.Info("Discovery service started")
	return nil
}

// Stop 停止发现服务
func (d *ServerBasedDiscovery) Stop() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if !d.isRunning {
		return nil
	}

	// 发送停止信号
	close(d.stopChan)

	// 关闭连接
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}

	// 标记为未运行
	d.isRunning = false

	log.Info("Discovery service stopped")
	return nil
}

// GetPeers 获取所有对等节点
func (d *ServerBasedDiscovery) GetPeers() []PeerInfo {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	peers := make([]PeerInfo, 0, len(d.peers))
	for _, peer := range d.peers {
		peers = append(peers, peer)
	}

	return peers
}

// GetPeer 获取指定对等节点
func (d *ServerBasedDiscovery) GetPeer(name string) (PeerInfo, bool) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	peer, exists := d.peers[name]
	return peer, exists
}

// AddListener 添加事件监听器
func (d *ServerBasedDiscovery) AddListener(listener PeerEventListener) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.listeners = append(d.listeners, listener)
}

// RemoveListener 移除事件监听器
func (d *ServerBasedDiscovery) RemoveListener(listener PeerEventListener) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	for i, l := range d.listeners {
		if l == listener {
			d.listeners = append(d.listeners[:i], d.listeners[i+1:]...)
			break
		}
	}
}

// UpdatePeerStatus 更新对等节点状态
func (d *ServerBasedDiscovery) UpdatePeerStatus(name string, isConnected bool) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	peer, exists := d.peers[name]
	if !exists {
		return errors.New("peer not found")
	}

	// 更新状态
	peer.IsConnected = isConnected
	peer.LastSeen = time.Now()
	d.peers[name] = peer

	// 触发事件
	d.notifyListeners(PeerEventData{
		EventType: PeerUpdated,
		Peer:      peer,
	})

	return nil
}

// receiveLoop 接收消息循环
func (d *ServerBasedDiscovery) receiveLoop() {
	buffer := make([]byte, 4096)

	for {
		select {
		case <-d.stopChan:
			return
		default:
			// 设置读取超时
			d.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			// 读取消息
			n, _, err := d.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}
				log.Error("Error reading from UDP: %v", err)
				continue
			}

			// 解析消息
			msg, err := message.Deserialize(buffer[:n])
			if err != nil {
				log.Error("Failed to deserialize message: %v", err)
				continue
			}

			// 处理消息
			d.handleMessage(msg)
		}
	}
}

// heartbeatLoop 心跳循环
func (d *ServerBasedDiscovery) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			// 发送心跳消息
			err := d.sendMessage(message.NewHeartbeatMessage())
			if err != nil {
				log.Error("Failed to send heartbeat: %v", err)
			}

			// 请求更新对等节点列表
			err = d.sendMessage(message.NewSearchAllMessage())
			if err != nil {
				log.Error("Failed to request peer list: %v", err)
			}

			// 清理过期的对等节点
			d.cleanupExpiredPeers()
		}
	}
}

// handleMessage 处理接收到的消息
func (d *ServerBasedDiscovery) handleMessage(msg *message.BaseMessage) {
	switch msg.GetType() {
	case message.SearchAll:
		// 处理对等节点列表
		d.handlePeerList(msg.Data)
	case message.ConnectAllow:
		// 处理连接允许消息
		d.handleConnectAllow(msg.Data)
	case message.ConnectDeny:
		// 处理连接拒绝消息
		d.handleConnectDeny(msg.Data)
	case message.ConnectTo:
		// 处理连接请求消息
		d.handleConnectTo(msg.Data)
	}
}

// handlePeerList 处理对等节点列表
func (d *ServerBasedDiscovery) handlePeerList(data string) {
	// 解析对等节点列表
	var peerList map[string]string
	err := json.Unmarshal([]byte(data), &peerList)
	if err != nil {
		log.Error("Failed to parse peer list: %v", err)
		return
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()

	// 记录当前对等节点
	currentPeers := make(map[string]bool)
	for name := range d.peers {
		currentPeers[name] = true
	}

	// 更新对等节点列表
	for name, addrStr := range peerList {
		// 跳过自己
		if name == d.clientName {
			continue
		}

		// 解析地址
		addr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			log.Error("Failed to resolve peer address: %v", err)
			continue
		}

		// 检查是否是新对等节点
		if _, exists := d.peers[name]; !exists {
			// 添加新对等节点
			peer := PeerInfo{
				Name:        name,
				Address:     addr,
				LastSeen:    time.Now(),
				IsConnected: false,
			}
			d.peers[name] = peer

			// 触发事件
			d.notifyListeners(PeerEventData{
				EventType: PeerAdded,
				Peer:      peer,
			})
		} else {
			// 更新现有对等节点
			peer := d.peers[name]
			peer.Address = addr
			peer.LastSeen = time.Now()
			d.peers[name] = peer
		}

		// 标记为已处理
		delete(currentPeers, name)
	}

	// 移除不再存在的对等节点
	for name := range currentPeers {
		peer := d.peers[name]
		delete(d.peers, name)

		// 触发事件
		d.notifyListeners(PeerEventData{
			EventType: PeerRemoved,
			Peer:      peer,
		})
	}
}

// handleConnectAllow 处理连接允许消息
func (d *ServerBasedDiscovery) handleConnectAllow(data string) {
	// 解析连接信息
	var connectInfo struct {
		PeerName  string `json:"peer_name"`
		PeerAddr  string `json:"peer_addr"`
		LocalAddr string `json:"local_addr"`
	}

	err := json.Unmarshal([]byte(data), &connectInfo)
	if err != nil {
		log.Error("Failed to parse connect info: %v", err)
		return
	}

	// 解析地址
	peerAddr, err := net.ResolveUDPAddr("udp", connectInfo.PeerAddr)
	if err != nil {
		log.Error("Failed to resolve peer address: %v", err)
		return
	}

	// 注意：我们不需要解析本地地址，因为我们只需要对等节点的地址

	// 更新对等节点信息
	d.mutex.Lock()
	peer, exists := d.peers[connectInfo.PeerName]
	if exists {
		peer.Address = peerAddr
		peer.LastSeen = time.Now()
		d.peers[connectInfo.PeerName] = peer
	} else {
		// 添加新对等节点
		peer = PeerInfo{
			Name:        connectInfo.PeerName,
			Address:     peerAddr,
			LastSeen:    time.Now(),
			IsConnected: false,
		}
		d.peers[connectInfo.PeerName] = peer
	}
	d.mutex.Unlock()

	// 触发事件
	d.notifyListeners(PeerEventData{
		EventType: PeerUpdated,
		Peer:      peer,
	})
}

// handleConnectDeny 处理连接拒绝消息
func (d *ServerBasedDiscovery) handleConnectDeny(data string) {
	// 解析对等节点名称
	peerName := data

	// 更新对等节点信息
	d.mutex.Lock()
	peer, exists := d.peers[peerName]
	if exists {
		peer.IsConnected = false
		peer.LastSeen = time.Now()
		d.peers[peerName] = peer

		// 触发事件
		d.notifyListeners(PeerEventData{
			EventType: PeerUpdated,
			Peer:      peer,
		})
	}
	d.mutex.Unlock()
}

// handleConnectTo 处理连接请求消息
func (d *ServerBasedDiscovery) handleConnectTo(data string) {
	// 解析对等节点名称
	peerName := data

	// 更新对等节点信息
	d.mutex.Lock()
	peer, exists := d.peers[peerName]
	if exists {
		peer.LastSeen = time.Now()
		d.peers[peerName] = peer

		// 触发事件
		d.notifyListeners(PeerEventData{
			EventType: PeerUpdated,
			Peer:      peer,
		})
	}
	d.mutex.Unlock()
}

// cleanupExpiredPeers 清理过期的对等节点
func (d *ServerBasedDiscovery) cleanupExpiredPeers() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	expireTime := time.Now().Add(-5 * time.Minute)
	for name, peer := range d.peers {
		if peer.LastSeen.Before(expireTime) {
			delete(d.peers, name)

			// 触发事件
			d.notifyListeners(PeerEventData{
				EventType: PeerRemoved,
				Peer:      peer,
			})
		}
	}
}

// sendMessage 发送消息到服务器
func (d *ServerBasedDiscovery) sendMessage(msg message.Message) error {
	d.mutex.RLock()
	conn := d.conn
	d.mutex.RUnlock()

	if conn == nil {
		return errors.New("connection not established")
	}

	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	_, err = conn.Write(data)
	return err
}

// notifyListeners 通知所有监听器
func (d *ServerBasedDiscovery) notifyListeners(event PeerEventData) {
	d.mutex.RLock()
	listeners := make([]PeerEventListener, len(d.listeners))
	copy(listeners, d.listeners)
	d.mutex.RUnlock()

	for _, listener := range listeners {
		listener.OnPeerEvent(event)
	}
}
