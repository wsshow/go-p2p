package message

import (
	"encoding/json"
)

// MessageType 定义消息类型的枚举
type MessageType uint8

const (
	// Heartbeat 心跳消息
	Heartbeat MessageType = iota
	// Connect 连接服务消息
	Connect
	// ConnectTo 连接指定客户端消息
	ConnectTo
	// ConnectAllow 指定客户端允许连接消息
	ConnectAllow
	// ConnectDeny 指定客户端拒绝连接消息
	ConnectDeny
	// Search 查询指定客户端消息
	Search
	// SearchAll 查询所有客户端消息
	SearchAll
	// Rename 客户端重命名
	Rename
	// ChangeToTCP 转为tcp连接
	ChangeToTCP
	// Msg 普通消息
	Msg
)

// Message 定义消息接口
type Message interface {
	// GetType 返回消息类型
	GetType() MessageType
	// Serialize 将消息序列化为字节数组
	Serialize() ([]byte, error)
}

// BaseMessage 实现基本消息结构
type BaseMessage struct {
	Type MessageType `json:"msgtype"`
	Data string      `json:"msg"`
}

// GetType 返回消息类型
func (m *BaseMessage) GetType() MessageType {
	return m.Type
}

// Serialize 将消息序列化为字节数组
func (m *BaseMessage) Serialize() ([]byte, error) {
	return json.Marshal(m)
}

// Deserialize 将字节数组反序列化为消息
func Deserialize(data []byte) (*BaseMessage, error) {
	var msg BaseMessage
	err := json.Unmarshal(data, &msg)
	return &msg, err
}

// NewMessage 创建一个新的消息
func NewMessage(msgType MessageType, data string) *BaseMessage {
	return &BaseMessage{
		Type: msgType,
		Data: data,
	}
}

// NewHeartbeatMessage 创建一个心跳消息
func NewHeartbeatMessage() *BaseMessage {
	return NewMessage(Heartbeat, "")
}

// NewConnectMessage 创建一个连接消息
func NewConnectMessage() *BaseMessage {
	return NewMessage(Connect, "")
}

// NewConnectToMessage 创建一个连接到指定客户端的消息
func NewConnectToMessage(clientName string) *BaseMessage {
	return NewMessage(ConnectTo, clientName)
}

// NewConnectAllowMessage 创建一个允许连接的消息
func NewConnectAllowMessage(clientName string) *BaseMessage {
	return NewMessage(ConnectAllow, clientName)
}

// NewConnectDenyMessage 创建一个拒绝连接的消息
func NewConnectDenyMessage(clientName string) *BaseMessage {
	return NewMessage(ConnectDeny, clientName)
}

// NewSearchMessage 创建一个查询指定客户端的消息
func NewSearchMessage(clientName string) *BaseMessage {
	return NewMessage(Search, clientName)
}

// NewSearchAllMessage 创建一个查询所有客户端的消息
func NewSearchAllMessage() *BaseMessage {
	return NewMessage(SearchAll, "")
}

// NewRenameMessage 创建一个重命名消息
func NewRenameMessage(newName string) *BaseMessage {
	return NewMessage(Rename, newName)
}

// NewChangeToTCPMessage 创建一个切换到TCP的消息
func NewChangeToTCPMessage() *BaseMessage {
	return NewMessage(ChangeToTCP, "")
}

// NewTextMessage 创建一个普通文本消息
func NewTextMessage(text string) *BaseMessage {
	return NewMessage(Msg, text)
}