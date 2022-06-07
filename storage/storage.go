package storage

type MsgType uint8

const (
	// 心跳消息
	Heartbeat MsgType = iota
	// 连接服务消息
	Connect
	// 连接指定客户端消息
	ConnectTo
	// 指定客户端允许连接消息
	ConnectAllow
	// 指定客户端拒绝连接消息
	ConnectDeny
	// 查询指定客户端消息
	Search
	// 查询所有客户端消息
	SearchAll
	// 客户端重命名
	Rename
	// 转为tcp连接
	ChangeToTCP
	// 普通消息
	Msg
)

type UserMsg struct {
	MsgType MsgType `json:"msgtype"`
	Msg     string  `json:"msg"`
}
