package nat

import (
	"errors"
	"net"
	"sync"
	"time"

	"go-p2p/internal/log"
)

// NATType 定义NAT类型
type NATType int

const (
	// Unknown 未知NAT类型
	Unknown NATType = iota
	// FullCone 完全锥形NAT
	FullCone
	// RestrictedCone 限制锥形NAT
	RestrictedCone
	// PortRestrictedCone 端口限制锥形NAT
	PortRestrictedCone
	// Symmetric 对称型NAT
	Symmetric
	// NoNAT 无NAT
	NoNAT
)

// String 返回NAT类型的字符串表示
func (t NATType) String() string {
	switch t {
	case FullCone:
		return "Full Cone NAT"
	case RestrictedCone:
		return "Restricted Cone NAT"
	case PortRestrictedCone:
		return "Port Restricted Cone NAT"
	case Symmetric:
		return "Symmetric NAT"
	case NoNAT:
		return "No NAT"
	default:
		return "Unknown NAT"
	}
}

// NATTraversal NAT穿透接口
type NATTraversal interface {
	// GetExternalAddress 获取外部地址
	GetExternalAddress() (*net.UDPAddr, error)
	// GetNATType 获取NAT类型
	GetNATType() (NATType, error)
	// PunchHole 打洞
	PunchHole(localAddr, remoteAddr *net.UDPAddr) error
}

// UDPHolePuncher UDP打洞实现
type UDPHolePuncher struct {
	stunServer  string      // STUN服务器地址
	natType     NATType     // NAT类型
	externalAddr *net.UDPAddr // 外部地址
	mutex       sync.RWMutex // 互斥锁
}

// NewUDPHolePuncher 创建新的UDP打洞器
func NewUDPHolePuncher(stunServer string) *UDPHolePuncher {
	return &UDPHolePuncher{
		stunServer: stunServer,
		natType:    Unknown,
	}
}

// GetExternalAddress 获取外部地址
func (p *UDPHolePuncher) GetExternalAddress() (*net.UDPAddr, error) {
	p.mutex.RLock()
	if p.externalAddr != nil {
		addr := *p.externalAddr // 复制一份
		p.mutex.RUnlock()
		return &addr, nil
	}
	p.mutex.RUnlock()

	// 如果没有缓存的外部地址，则进行NAT类型检测
	_, err := p.GetNATType()
	if err != nil {
		return nil, err
	}

	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.externalAddr == nil {
		return nil, errors.New("failed to determine external address")
	}

	addr := *p.externalAddr // 复制一份
	return &addr, nil
}

// GetNATType 获取NAT类型
func (p *UDPHolePuncher) GetNATType() (NATType, error) {
	p.mutex.RLock()
	if p.natType != Unknown {
		natType := p.natType
		p.mutex.RUnlock()
		return natType, nil
	}
	p.mutex.RUnlock()

	// 这里应该实现完整的STUN协议来检测NAT类型
	// 简化版本：只检测是否有外部地址
	externalAddr, err := p.detectExternalAddress()
	if err != nil {
		return Unknown, err
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 存储外部地址
	p.externalAddr = externalAddr

	// 简化版本：假设是端口限制锥形NAT
	p.natType = PortRestrictedCone

	return p.natType, nil
}

// PunchHole 打洞
func (p *UDPHolePuncher) PunchHole(localAddr, remoteAddr *net.UDPAddr) error {
	// 创建UDP连接
	conn, err := net.DialUDP("udp", localAddr, remoteAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 发送打洞数据包
	punchData := []byte("NAT_PUNCH")
	for i := 0; i < 5; i++ {
		_, err = conn.Write(punchData)
		if err != nil {
			log.Warn("Failed to send punch packet %d: %v", i+1, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Info("Sent NAT punch packets to %s", remoteAddr.String())
	return nil
}

// detectExternalAddress 检测外部地址
func (p *UDPHolePuncher) detectExternalAddress() (*net.UDPAddr, error) {
	// 这里应该实现完整的STUN协议
	// 简化版本：使用服务器告知的地址作为外部地址
	return nil, errors.New("STUN protocol not implemented")
}

// 全局NAT穿透实例
var (
	defaultTraversal NATTraversal
	once            sync.Once
)

// GetNATTraversal 获取NAT穿透实例
func GetNATTraversal() NATTraversal {
	once.Do(func() {
		// 使用默认STUN服务器
		defaultTraversal = NewUDPHolePuncher("stun.l.google.com:19302")
	})
	return defaultTraversal
}

// SetNATTraversal 设置NAT穿透实例
func SetNATTraversal(traversal NATTraversal) {
	defaultTraversal = traversal
}