package log

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// LogLevel 定义日志级别
type LogLevel int

const (
	// DEBUG 调试级别
	DEBUG LogLevel = iota
	// INFO 信息级别
	INFO
	// WARN 警告级别
	WARN
	// ERROR 错误级别
	ERROR
	// FATAL 致命错误级别
	FATAL
)

// Logger 定义日志接口
type Logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
	Fatal(format string, v ...interface{})
	SetLevel(level LogLevel)
	SetPrefix(prefix string)
}

// StandardLogger 标准日志实现
type StandardLogger struct {
	logger *log.Logger
	level  LogLevel
	mutex  sync.Mutex
}

var (
	// 单例实例
	instance *StandardLogger
	once     sync.Once
)

// GetLogger 获取日志实例（单例模式）
func GetLogger() Logger {
	once.Do(func() {
		instance = &StandardLogger{
			logger: log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lshortfile),
			level:  INFO,
		}
	})
	return instance
}

// Debug 记录调试级别日志
func (l *StandardLogger) Debug(format string, v ...interface{}) {
	if l.level <= DEBUG {
		l.output("[DEBUG] "+format, v...)
	}
}

// Info 记录信息级别日志
func (l *StandardLogger) Info(format string, v ...interface{}) {
	if l.level <= INFO {
		l.output("[INFO] "+format, v...)
	}
}

// Warn 记录警告级别日志
func (l *StandardLogger) Warn(format string, v ...interface{}) {
	if l.level <= WARN {
		l.output("[WARN] "+format, v...)
	}
}

// Error 记录错误级别日志
func (l *StandardLogger) Error(format string, v ...interface{}) {
	if l.level <= ERROR {
		l.output("[ERROR] "+format, v...)
	}
}

// Fatal 记录致命错误级别日志
func (l *StandardLogger) Fatal(format string, v ...interface{}) {
	if l.level <= FATAL {
		l.output("[FATAL] "+format, v...)
		os.Exit(1)
	}
}

// SetLevel 设置日志级别
func (l *StandardLogger) SetLevel(level LogLevel) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.level = level
}

// SetPrefix 设置日志前缀
func (l *StandardLogger) SetPrefix(prefix string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.logger.SetPrefix(prefix + " ")
}

// output 输出日志
func (l *StandardLogger) output(format string, v ...interface{}) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.logger.Output(3, fmt.Sprintf(format, v...))
}

// 提供包级别的便捷函数

// Debug 包级别调试日志
func Debug(format string, v ...interface{}) {
	GetLogger().Debug(format, v...)
}

// Info 包级别信息日志
func Info(format string, v ...interface{}) {
	GetLogger().Info(format, v...)
}

// Warn 包级别警告日志
func Warn(format string, v ...interface{}) {
	GetLogger().Warn(format, v...)
}

// Error 包级别错误日志
func Error(format string, v ...interface{}) {
	GetLogger().Error(format, v...)
}

// Fatal 包级别致命错误日志
func Fatal(format string, v ...interface{}) {
	GetLogger().Fatal(format, v...)
}

// SetLevel 包级别设置日志级别
func SetLevel(level LogLevel) {
	GetLogger().SetLevel(level)
}

// SetPrefix 包级别设置日志前缀
func SetPrefix(prefix string) {
	GetLogger().SetPrefix(prefix)
}