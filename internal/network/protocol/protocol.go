package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

// 协议常量
const (
	// MaxMessageSize 最大消息大小 (4MB)
	MaxMessageSize = 4 * 1024 * 1024

	// HeaderSize 消息头大小 (4字节)
	HeaderSize = 4
)

// 错误定义
var (
	// ErrMessageTooLarge 消息过大错误
	ErrMessageTooLarge = errors.New("message too large")

	// ErrInvalidMessage 无效消息错误
	ErrInvalidMessage = errors.New("invalid message")
)

// WriteMessage 写入消息
func WriteMessage(writer io.Writer, data []byte) error {
	// 检查消息大小
	if len(data) > MaxMessageSize {
		return ErrMessageTooLarge
	}

	// 写入消息长度
	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(header, uint32(len(data)))

	// 写入头部
	_, err := writer.Write(header)
	if err != nil {
		return err
	}

	// 写入消息内容
	_, err = writer.Write(data)
	return err
}

// ReadMessage 读取消息
func ReadMessage(reader io.Reader) ([]byte, error) {
	// 读取消息长度
	header := make([]byte, HeaderSize)
	_, err := io.ReadFull(reader, header)
	if err != nil {
		return nil, err
	}

	// 解析消息长度
	length := binary.BigEndian.Uint32(header)

	// 检查消息大小
	if length > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	// 读取消息内容
	data := make([]byte, length)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return nil, err
	}

	return data, nil
}
