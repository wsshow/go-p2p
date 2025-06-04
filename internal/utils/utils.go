package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// GenerateRandomString 生成指定长度的随机字符串
func GenerateRandomString(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[0:length]
		}
		result[i] = letters[num.Int64()]
	}
	return string(result)
}

// IsValidPort 检查端口是否有效
func IsValidPort(port int) bool {
	return port >= 0 && port <= 65535
}

// IsValidIPAddress 检查IP地址是否有效
func IsValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// ParseIPAndPort 解析IP地址和端口
func ParseIPAndPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}

	// 解析端口
	port := 0
	if portStr != "" {
		portBig, ok := new(big.Int).SetString(portStr, 10)
		if !ok {
			return "", 0, &net.AddrError{Err: "invalid port", Addr: addr}
		}
		port = int(portBig.Int64())
		if port < 0 || port > 65535 {
			return "", 0, &net.AddrError{Err: "invalid port", Addr: addr}
		}
	}

	return host, port, nil
}

// FormatDuration 格式化持续时间
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	} else if d < time.Hour {
		m := d / time.Minute
		s := (d % time.Minute) / time.Second
		return fmt.Sprintf("%dm%ds", m, s)
	} else {
		h := d / time.Hour
		m := (d % time.Hour) / time.Minute
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// TruncateString 截断字符串到指定长度
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// SanitizeInput 清理用户输入
func SanitizeInput(input string) string {
	// 移除前后空白
	input = strings.TrimSpace(input)
	// 移除控制字符
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 { // ASCII控制字符
			return -1
		}
		return r
	}, input)
}

// ParseCommand 解析命令和参数
func ParseCommand(input string) (cmd string, args []string) {
	// 清理输入
	input = SanitizeInput(input)
	if input == "" {
		return "", nil
	}

	// 分割命令和参数
	parts := strings.SplitN(input, " ", 2)
	cmd = strings.ToLower(parts[0])

	if len(parts) > 1 && parts[1] != "" {
		// 处理参数
		args = []string{parts[1]}
	}

	return cmd, args
}