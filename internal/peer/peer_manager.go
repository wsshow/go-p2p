package peer

import (
	"errors"
	"net"
	"sync"

	"go-p2p/internal/log"
	"go-p2p/internal/message"
	"go-p2p/internal/network/connection"
)

// PeerInfo 存储对等节点信息
type PeerInfo struct {
	Name       string                // 节点名称
	Address    *net.UDPAddr          // 节点UDP地址
	Connection connection.Connection // 与节点的连接
}

// PeerEventType 定义对等节点事件类型
type PeerEventType int

const (
	// PeerConnected 节点连接事件
	PeerConnected PeerEventType = iota
	// PeerDisconnected 节点断开连接事件
	PeerDisconnected
	// PeerMessageReceived 收到节点消息事件
	PeerMessageReceived
)

// PeerEvent 定义对等节点事件
type PeerEvent struct {
	Type    PeerEventType
	Peer    *PeerInfo
	Message *message.BaseMessage // 可选，仅当Type为PeerMessageReceived时有效
}

// PeerEventListener 定义对等节点事件监听器
type PeerEventListener interface {
	OnPeerEvent(event PeerEvent)
}

// PeerManager 对等节点管理器
type PeerManager struct {
	peers       map[string]*PeerInfo
	listeners   []PeerEventListener
	mutex       sync.RWMutex
	connFactory connection.ConnectionFactory
}

// NewPeerManager 创建新的对等节点管理器
func NewPeerManager(clientName string) *PeerManager {
	return &PeerManager{
		peers:       make(map[string]*PeerInfo),
		listeners:   make([]PeerEventListener, 0),
		connFactory: connection.GetConnectionFactory(),
	}
}

// AddPeer 添加对等节点
func (pm *PeerManager) AddPeer(name string, addr *net.UDPAddr) *PeerInfo {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// 检查是否已存在
	if peer, exists := pm.peers[name]; exists {
		return peer
	}

	// 创建新的对等节点信息
	peer := &PeerInfo{
		Name:    name,
		Address: addr,
	}

	// 添加到管理器
	pm.peers[name] = peer
	log.Info("Added peer: %s at %s", name, addr.String())

	return peer
}

// RemovePeer 移除对等节点
func (pm *PeerManager) RemovePeer(name string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if peer, exists := pm.peers[name]; exists {
		// 关闭连接
		if peer.Connection != nil {
			peer.Connection.Close()
		}

		// 从管理器中移除
		delete(pm.peers, name)
		log.Info("Removed peer: %s", name)

		// 触发事件
		pm.notifyListeners(PeerEvent{
			Type: PeerDisconnected,
			Peer: peer,
		})
	}
}

// GetPeer 获取对等节点
func (pm *PeerManager) GetPeer(name string) (*PeerInfo, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	peer, exists := pm.peers[name]
	return peer, exists
}

// GetAllPeers 获取所有对等节点
func (pm *PeerManager) GetAllPeers() []*PeerInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	peers := make([]*PeerInfo, 0, len(pm.peers))
	for _, peer := range pm.peers {
		peers = append(peers, peer)
	}

	return peers
}

// EstablishUDPConnection 建立UDP连接
func (pm *PeerManager) EstablishUDPConnection(name string, localAddr, remoteAddr *net.UDPAddr) error {
	// 获取对等节点
	peer, exists := pm.GetPeer(name)
	if !exists {
		return errors.New("peer not found")
	}

	// 创建UDP连接
	udpConn, err := net.DialUDP("udp", localAddr, remoteAddr)
	if err != nil {
		return err
	}

	// 创建连接对象
	conn := pm.connFactory.CreateUDPConnection(udpConn, remoteAddr)

	// 设置消息处理器
	conn.SetMessageHandler(func(msg *message.BaseMessage, c connection.Connection) {
		pm.handlePeerMessage(name, msg)
	})

	// 更新对等节点信息
	pm.mutex.Lock()
	peer.Connection = conn
	pm.mutex.Unlock()

	// 触发连接事件
	pm.notifyListeners(PeerEvent{
		Type: PeerConnected,
		Peer: peer,
	})

	log.Info("Established UDP connection with peer: %s", name)
	return nil
}

// EstablishTCPConnection 建立TCP连接
func (pm *PeerManager) EstablishTCPConnection(name string, conn *net.TCPConn) error {
	// 获取对等节点
	peer, exists := pm.GetPeer(name)
	if !exists {
		return errors.New("peer not found")
	}

	// 创建连接对象
	tcpConn := pm.connFactory.CreateTCPConnection(conn)

	// 设置消息处理器
	tcpConn.SetMessageHandler(func(msg *message.BaseMessage, c connection.Connection) {
		pm.handlePeerMessage(name, msg)
	})

	// 关闭旧连接
	pm.mutex.Lock()
	if peer.Connection != nil {
		peer.Connection.Close()
	}

	// 更新对等节点信息
	peer.Connection = tcpConn
	pm.mutex.Unlock()

	// 触发连接事件
	pm.notifyListeners(PeerEvent{
		Type: PeerConnected,
		Peer: peer,
	})

	log.Info("Established TCP connection with peer: %s", name)
	return nil
}

// SendMessage 向对等节点发送消息
func (pm *PeerManager) SendMessage(name string, msg message.Message) error {
	// 获取对等节点
	peer, exists := pm.GetPeer(name)
	if !exists {
		return errors.New("peer not found")
	}

	// 检查连接
	pm.mutex.RLock()
	conn := peer.Connection
	pm.mutex.RUnlock()

	if conn == nil || !conn.IsActive() {
		return errors.New("peer connection not active")
	}

	// 发送消息
	return conn.Send(msg)
}

// AddListener 添加事件监听器
func (pm *PeerManager) AddListener(listener PeerEventListener) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.listeners = append(pm.listeners, listener)
}

// RemoveListener 移除事件监听器
func (pm *PeerManager) RemoveListener(listener PeerEventListener) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	for i, l := range pm.listeners {
		if l == listener {
			// 移除监听器
			pm.listeners = append(pm.listeners[:i], pm.listeners[i+1:]...)
			break
		}
	}
}

// handlePeerMessage 处理来自对等节点的消息
func (pm *PeerManager) handlePeerMessage(peerName string, msg *message.BaseMessage) {
	// 获取对等节点
	peer, exists := pm.GetPeer(peerName)
	if !exists {
		log.Warn("Received message from unknown peer: %s", peerName)
		return
	}

	// 触发消息事件
	pm.notifyListeners(PeerEvent{
		Type:    PeerMessageReceived,
		Peer:    peer,
		Message: msg,
	})
}

// notifyListeners 通知所有监听器
func (pm *PeerManager) notifyListeners(event PeerEvent) {
	pm.mutex.RLock()
	listeners := make([]PeerEventListener, len(pm.listeners))
	copy(listeners, pm.listeners)
	pm.mutex.RUnlock()

	for _, listener := range listeners {
		listener.OnPeerEvent(event)
	}
}

// Shutdown 关闭对等节点管理器
func (pm *PeerManager) Shutdown() error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// 关闭所有连接
	for name, peer := range pm.peers {
		if peer.Connection != nil {
			peer.Connection.Close()
			log.Info("Closed connection with peer: %s", name)
		}
	}

	// 清空对等节点列表
	pm.peers = make(map[string]*PeerInfo)
	return nil
}

// ConnectToPeer 连接到对等节点
func (pm *PeerManager) ConnectToPeer(name string, addr *net.UDPAddr) error {
	// 检查对等节点是否已存在
	_, exists := pm.GetPeer(name)
	if !exists {
		// 添加对等节点
		_ = pm.AddPeer(name, addr)
	}

	// 创建本地UDP地址
	localAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}

	// 建立UDP连接
	return pm.EstablishUDPConnection(name, localAddr, addr)
}

// DisconnectPeer 断开与对等节点的连接
func (pm *PeerManager) DisconnectPeer(name string) error {
	// 获取对等节点
	peer, exists := pm.GetPeer(name)
	if !exists {
		return errors.New("peer not found")
	}

	// 检查连接
	pm.mutex.RLock()
	conn := peer.Connection
	pm.mutex.RUnlock()

	if conn == nil || !conn.IsActive() {
		return errors.New("peer connection not active")
	}

	// 关闭连接
	err := conn.Close()
	if err != nil {
		return err
	}

	// 更新对等节点信息
	pm.mutex.Lock()
	peer.Connection = nil
	pm.mutex.Unlock()

	// 触发断开连接事件
	pm.notifyListeners(PeerEvent{
		Type: PeerDisconnected,
		Peer: peer,
	})

	log.Info("Disconnected from peer: %s", name)
	return nil
}

// IsPeerConnected 检查对等节点是否已连接
func (pm *PeerManager) IsPeerConnected(name string) bool {
	// 获取对等节点
	peer, exists := pm.GetPeer(name)
	if !exists {
		return false
	}

	// 检查连接
	pm.mutex.RLock()
	conn := peer.Connection
	pm.mutex.RUnlock()

	return conn != nil && conn.IsActive()
}
