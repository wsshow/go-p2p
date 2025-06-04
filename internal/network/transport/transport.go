package transport

import (
	"errors"
	"net"
	"sync"
	"time"

	"go-p2p/internal/log"
	"go-p2p/internal/message"
	"go-p2p/internal/network/protocol"
)

// 错误定义
var (
	// ErrTransportClosed 传输已关闭错误
	ErrTransportClosed = errors.New("transport closed")

	// ErrInvalidAddress 无效地址错误
	ErrInvalidAddress = errors.New("invalid address")
)

// MessageHandler 消息处理函数类型
type MessageHandler func(msg *message.BaseMessage, addr net.Addr)

// Transport 网络传输接口
type Transport interface {
	// Start 启动传输
	Start() error

	// Stop 停止传输
	Stop() error

	// Send 发送消息
	Send(msg message.Message, addr net.Addr) error

	// SetMessageHandler 设置消息处理器
	SetMessageHandler(handler MessageHandler)

	// GetLocalAddr 获取本地地址
	GetLocalAddr() net.Addr
}

// UDPTransport UDP传输实现
type UDPTransport struct {
	conn           *net.UDPConn   // UDP连接
	messageHandler MessageHandler // 消息处理器
	isRunning      bool           // 是否正在运行
	stopChan       chan struct{}  // 停止信号通道
	mutex          sync.RWMutex   // 互斥锁
}

// NewUDPTransport 创建新的UDP传输
func NewUDPTransport(localAddr *net.UDPAddr) (*UDPTransport, error) {
	// 创建UDP连接
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		return nil, err
	}

	return &UDPTransport{
		conn:      conn,
		stopChan:  make(chan struct{}),
		isRunning: false,
	}, nil
}

// Start 启动UDP传输
func (t *UDPTransport) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.isRunning {
		return nil
	}

	t.isRunning = true
	go t.receiveLoop()

	log.Info("UDP transport started on %s", t.conn.LocalAddr().String())
	return nil
}

// Stop 停止UDP传输
func (t *UDPTransport) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.isRunning {
		return nil
	}

	t.isRunning = false
	close(t.stopChan)
	err := t.conn.Close()

	log.Info("UDP transport stopped")
	return err
}

// Send 发送消息
func (t *UDPTransport) Send(msg message.Message, addr net.Addr) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if !t.isRunning {
		return ErrTransportClosed
	}

	// 序列化消息
	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	// 发送消息
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return ErrInvalidAddress
	}

	_, err = t.conn.WriteToUDP(data, udpAddr)
	return err
}

// SetMessageHandler 设置消息处理器
func (t *UDPTransport) SetMessageHandler(handler MessageHandler) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.messageHandler = handler
}

// GetLocalAddr 获取本地地址
func (t *UDPTransport) GetLocalAddr() net.Addr {
	return t.conn.LocalAddr()
}

// receiveLoop 接收循环
func (t *UDPTransport) receiveLoop() {
	buffer := make([]byte, 65536) // 64KB缓冲区

	for {
		select {
		case <-t.stopChan:
			return
		default:
			// 设置读取超时
			t.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

			// 读取数据
			n, addr, err := t.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}

				if t.isRunning {
					log.Error("Error reading from UDP: %v", err)
				}
				continue
			}

			// 处理消息
			go t.handleMessage(buffer[:n], addr)
		}
	}
}

// handleMessage 处理接收到的消息
func (t *UDPTransport) handleMessage(data []byte, addr net.Addr) {
	// 反序列化消息
	msg, err := message.Deserialize(data)
	if err != nil {
		log.Error("Failed to deserialize message: %v", err)
		return
	}

	// 调用消息处理器
	t.mutex.RLock()
	handler := t.messageHandler
	t.mutex.RUnlock()

	if handler != nil {
		handler(msg, addr)
	}
}

// TCPTransport TCP传输实现
type TCPTransport struct {
	listener       net.Listener        // TCP监听器
	connections    map[string]net.Conn // 连接映射
	messageHandler MessageHandler      // 消息处理器
	isRunning      bool                // 是否正在运行
	stopChan       chan struct{}       // 停止信号通道
	mutex          sync.RWMutex        // 互斥锁
}

// NewTCPTransport 创建新的TCP传输
func NewTCPTransport(localAddr string) (*TCPTransport, error) {
	// 创建TCP监听器
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, err
	}

	return &TCPTransport{
		listener:    listener,
		connections: make(map[string]net.Conn),
		stopChan:    make(chan struct{}),
		isRunning:   false,
	}, nil
}

// Start 启动TCP传输
func (t *TCPTransport) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.isRunning {
		return nil
	}

	t.isRunning = true
	go t.acceptLoop()

	log.Info("TCP transport started on %s", t.listener.Addr().String())
	return nil
}

// Stop 停止TCP传输
func (t *TCPTransport) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.isRunning {
		return nil
	}

	t.isRunning = false
	close(t.stopChan)

	// 关闭所有连接
	for _, conn := range t.connections {
		conn.Close()
	}

	// 清空连接映射
	t.connections = make(map[string]net.Conn)

	// 关闭监听器
	err := t.listener.Close()

	log.Info("TCP transport stopped")
	return err
}

// Send 发送消息
func (t *TCPTransport) Send(msg message.Message, addr net.Addr) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if !t.isRunning {
		return ErrTransportClosed
	}

	// 获取连接
	conn, exists := t.connections[addr.String()]
	if !exists {
		return errors.New("connection not found")
	}

	// 序列化消息
	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	// 发送消息
	return protocol.WriteMessage(conn, data)
}

// SetMessageHandler 设置消息处理器
func (t *TCPTransport) SetMessageHandler(handler MessageHandler) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.messageHandler = handler
}

// GetLocalAddr 获取本地地址
func (t *TCPTransport) GetLocalAddr() net.Addr {
	return t.listener.Addr()
}

// acceptLoop 接受连接循环
func (t *TCPTransport) acceptLoop() {
	for {
		select {
		case <-t.stopChan:
			return
		default:
			// 设置接受超时
			t.listener.(*net.TCPListener).SetDeadline(time.Now().Add(500 * time.Millisecond))

			// 接受连接
			conn, err := t.listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}

				if t.isRunning {
					log.Error("Error accepting TCP connection: %v", err)
				}
				continue
			}

			// 处理连接
			go t.handleConnection(conn)
		}
	}
}

// handleConnection 处理TCP连接
func (t *TCPTransport) handleConnection(conn net.Conn) {
	// 添加到连接映射
	t.mutex.Lock()
	t.connections[conn.RemoteAddr().String()] = conn
	t.mutex.Unlock()

	log.Info("New TCP connection from %s", conn.RemoteAddr().String())

	// 读取消息循环
	for {
		select {
		case <-t.stopChan:
			return
		default:
			// 设置读取超时
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

			// 读取消息
			data, err := protocol.ReadMessage(conn)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时，继续循环
					continue
				}

				// 连接关闭或出错
				log.Info("TCP connection closed: %v", err)

				// 从连接映射中移除
				t.mutex.Lock()
				delete(t.connections, conn.RemoteAddr().String())
				t.mutex.Unlock()

				// 关闭连接
				conn.Close()
				return
			}

			// 处理消息
			t.handleMessage(data, conn.RemoteAddr())
		}
	}
}

// handleMessage 处理接收到的消息
func (t *TCPTransport) handleMessage(data []byte, addr net.Addr) {
	// 反序列化消息
	msg, err := message.Deserialize(data)
	if err != nil {
		log.Error("Failed to deserialize message: %v", err)
		return
	}

	// 调用消息处理器
	t.mutex.RLock()
	handler := t.messageHandler
	t.mutex.RUnlock()

	if handler != nil {
		handler(msg, addr)
	}
}
