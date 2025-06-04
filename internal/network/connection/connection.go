package connection

import (
	"errors"
	"net"
	"sync"
	"time"

	"go-p2p/internal/log"
	"go-p2p/internal/message"
)

// ConnectionType 定义连接类型
type ConnectionType string

const (
	// UDP 连接类型
	UDP ConnectionType = "UDP"
	// TCP 连接类型
	TCP ConnectionType = "TCP"
)

// Connection 定义连接接口
type Connection interface {
	// Send 发送消息
	Send(msg message.Message) error
	// Close 关闭连接
	Close() error
	// GetType 获取连接类型
	GetType() ConnectionType
	// GetRemoteAddr 获取远程地址
	GetRemoteAddr() net.Addr
	// GetLocalAddr 获取本地地址
	GetLocalAddr() net.Addr
	// IsActive 检查连接是否活跃
	IsActive() bool
	// SetMessageHandler 设置消息处理器
	SetMessageHandler(handler MessageHandler)
}

// MessageHandler 定义消息处理函数
type MessageHandler func(msg *message.BaseMessage, conn Connection)

// BaseConnection 基础连接实现
type BaseConnection struct {
	connType       ConnectionType
	isActive       bool
	mutex          sync.RWMutex
	messageHandler MessageHandler
}

// Close 关闭基础连接
func (c *BaseConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.isActive = false
	return nil
}

// GetLocalAddr 获取本地地址（基类实现返回nil，子类需要重写）
func (c *BaseConnection) GetLocalAddr() net.Addr {
	return nil
}

// GetRemoteAddr 获取远程地址（基类实现返回nil，子类需要重写）
func (c *BaseConnection) GetRemoteAddr() net.Addr {
	return nil
}

// Send 发送消息（基类实现返回错误，子类需要重写）
func (c *BaseConnection) Send(msg message.Message) error {
	return errors.New("send method not implemented in base connection")
}

// GetType 获取连接类型
func (c *BaseConnection) GetType() ConnectionType {
	return c.connType
}

// IsActive 检查连接是否活跃
func (c *BaseConnection) IsActive() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isActive
}

// SetMessageHandler 设置消息处理器
func (c *BaseConnection) SetMessageHandler(handler MessageHandler) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.messageHandler = handler
}

// handleMessage 处理接收到的消息
func (c *BaseConnection) handleMessage(msg *message.BaseMessage) {
	c.mutex.RLock()
	handler := c.messageHandler
	c.mutex.RUnlock()

	if handler != nil {
		handler(msg, c)
	}
}

// UDPConnection UDP连接实现
type UDPConnection struct {
	BaseConnection
	conn        *net.UDPConn
	remoteAddr  *net.UDPAddr
	localAddr   *net.UDPAddr
	readTimeout time.Duration
}

// NewUDPConnection 创建新的UDP连接
func NewUDPConnection(conn *net.UDPConn, remoteAddr *net.UDPAddr) *UDPConnection {
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		log.Error("Failed to assert LocalAddr to *net.UDPAddr")
		return nil
	}

	udpConn := &UDPConnection{
		BaseConnection: BaseConnection{
			connType: UDP,
			isActive: true,
		},
		conn:        conn,
		remoteAddr:  remoteAddr,
		localAddr:   localAddr,
		readTimeout: 30 * time.Second,
	}

	// 启动接收协程
	go udpConn.receiveLoop()

	return udpConn
}

// Send 发送UDP消息
func (c *UDPConnection) Send(msg message.Message) error {
	c.mutex.RLock()
	isActive := c.isActive
	c.mutex.RUnlock()

	if !isActive {
		return errors.New("connection is not active")
	}

	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	_, err = c.conn.WriteToUDP(data, c.remoteAddr)
	return err
}

// Close 关闭UDP连接
func (c *UDPConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.isActive {
		return nil
	}

	c.isActive = false
	return c.conn.Close()
}

// GetRemoteAddr 获取远程地址
func (c *UDPConnection) GetRemoteAddr() net.Addr {
	return c.remoteAddr
}

// GetLocalAddr 获取本地地址
func (c *UDPConnection) GetLocalAddr() net.Addr {
	return c.localAddr
}

// receiveLoop 接收UDP消息的循环
func (c *UDPConnection) receiveLoop() {
	buffer := make([]byte, 4096)

	for {
		c.mutex.RLock()
		isActive := c.isActive
		c.mutex.RUnlock()

		if !isActive {
			break
		}

		// 设置读取超时
		c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))

		n, addr, err := c.conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 超时，继续循环
				continue
			}
			log.Error("Error reading from UDP: %v", err)
			break
		}

		// 验证消息来源
		if addr.String() != c.remoteAddr.String() {
			log.Warn("Received UDP packet from unexpected address: %s", addr.String())
			continue
		}

		// 解析消息
		msg, err := message.Deserialize(buffer[:n])
		if err != nil {
			log.Error("Failed to deserialize message: %v", err)
			continue
		}

		// 处理消息
		c.handleMessage(msg)
	}
}

// TCPConnection TCP连接实现
type TCPConnection struct {
	BaseConnection
	conn       *net.TCPConn
	remoteAddr *net.TCPAddr
	localAddr  *net.TCPAddr
}

// NewTCPConnection 创建新的TCP连接
func NewTCPConnection(conn *net.TCPConn) *TCPConnection {
	remoteAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		log.Error("Failed to assert RemoteAddr to *net.TCPAddr")
		return nil
	}

	localAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		log.Error("Failed to assert LocalAddr to *net.TCPAddr")
		return nil
	}

	tcpConn := &TCPConnection{
		BaseConnection: BaseConnection{
			connType: TCP,
			isActive: true,
		},
		conn:       conn,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
	}

	// 启动接收协程
	go tcpConn.receiveLoop()

	return tcpConn
}

// Send 发送TCP消息
func (c *TCPConnection) Send(msg message.Message) error {
	c.mutex.RLock()
	isActive := c.isActive
	c.mutex.RUnlock()

	if !isActive {
		return errors.New("connection is not active")
	}

	data, err := msg.Serialize()
	if err != nil {
		return err
	}

	// 添加消息长度前缀，便于接收方解析
	lenBuf := make([]byte, 4)
	lenBuf[0] = byte(len(data) >> 24)
	lenBuf[1] = byte(len(data) >> 16)
	lenBuf[2] = byte(len(data) >> 8)
	lenBuf[3] = byte(len(data))

	// 先发送长度
	_, err = c.conn.Write(lenBuf)
	if err != nil {
		return err
	}

	// 再发送数据
	_, err = c.conn.Write(data)
	return err
}

// Close 关闭TCP连接
func (c *TCPConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.isActive {
		return nil
	}

	c.isActive = false
	return c.conn.Close()
}

// GetRemoteAddr 获取远程地址
func (c *TCPConnection) GetRemoteAddr() net.Addr {
	return c.remoteAddr
}

// GetLocalAddr 获取本地地址
func (c *TCPConnection) GetLocalAddr() net.Addr {
	return c.localAddr
}

// receiveLoop 接收TCP消息的循环
func (c *TCPConnection) receiveLoop() {
	for {
		c.mutex.RLock()
		isActive := c.isActive
		c.mutex.RUnlock()

		if !isActive {
			break
		}

		// 读取消息长度
		lenBuf := make([]byte, 4)
		_, err := c.conn.Read(lenBuf)
		if err != nil {
			log.Error("Error reading message length: %v", err)
			c.Close()
			break
		}

		// 解析消息长度
		msgLen := int(lenBuf[0])<<24 | int(lenBuf[1])<<16 | int(lenBuf[2])<<8 | int(lenBuf[3])
		if msgLen <= 0 || msgLen > 65536 { // 限制最大消息大小
			log.Error("Invalid message length: %d", msgLen)
			c.Close()
			break
		}

		// 读取消息内容
		buffer := make([]byte, msgLen)
		_, err = c.conn.Read(buffer)
		if err != nil {
			log.Error("Error reading message content: %v", err)
			c.Close()
			break
		}

		// 解析消息
		msg, err := message.Deserialize(buffer)
		if err != nil {
			log.Error("Failed to deserialize message: %v", err)
			continue
		}

		// 处理消息
		c.handleMessage(msg)
	}
}

// ConnectionFactory 连接工厂接口
type ConnectionFactory interface {
	// CreateUDPConnection 创建UDP连接
	CreateUDPConnection(conn *net.UDPConn, remoteAddr *net.UDPAddr) Connection
	// CreateTCPConnection 创建TCP连接
	CreateTCPConnection(conn *net.TCPConn) Connection
}

// DefaultConnectionFactory 默认连接工厂实现
type DefaultConnectionFactory struct{}

// CreateUDPConnection 创建UDP连接
func (f *DefaultConnectionFactory) CreateUDPConnection(conn *net.UDPConn, remoteAddr *net.UDPAddr) Connection {
	return NewUDPConnection(conn, remoteAddr)
}

// CreateTCPConnection 创建TCP连接
func (f *DefaultConnectionFactory) CreateTCPConnection(conn *net.TCPConn) Connection {
	return NewTCPConnection(conn)
}

// 全局连接工厂实例
var defaultFactory ConnectionFactory = &DefaultConnectionFactory{}

// GetConnectionFactory 获取连接工厂实例
func GetConnectionFactory() ConnectionFactory {
	return defaultFactory
}

// SetConnectionFactory 设置连接工厂实例
func SetConnectionFactory(factory ConnectionFactory) {
	defaultFactory = factory
}
