package terminal

import (
	"fmt"
	"strings"

	"github.com/c-bata/go-prompt"
)

// CommandSuggestion 命令建议
type CommandSuggestion struct {
	Text        string // 命令文本
	Description string // 命令描述
}

// CommandHandler 命令处理函数
type CommandHandler func(args []string) error

// Terminal 终端界面
type Terminal struct {
	promptPrefix   string                     // 提示符前缀
	suggestions    []prompt.Suggest           // 命令建议
	commandHandlers map[string]CommandHandler // 命令处理器
	exitHandler    func()                     // 退出处理器
}

// NewTerminal 创建新的终端界面
func NewTerminal() *Terminal {
	return &Terminal{
		promptPrefix:   "go-p2p> ",
		suggestions:    []prompt.Suggest{},
		commandHandlers: make(map[string]CommandHandler),
		exitHandler:    func() {},
	}
}

// SetPromptPrefix 设置提示符前缀
func (t *Terminal) SetPromptPrefix(prefix string) {
	t.promptPrefix = prefix
}

// AddCommand 添加命令
func (t *Terminal) AddCommand(cmd CommandSuggestion, handler CommandHandler) {
	// 添加到建议列表
	t.suggestions = append(t.suggestions, prompt.Suggest{
		Text:        cmd.Text,
		Description: cmd.Description,
	})

	// 添加处理器
	t.commandHandlers[cmd.Text] = handler
}

// SetExitHandler 设置退出处理器
func (t *Terminal) SetExitHandler(handler func()) {
	t.exitHandler = handler
}

// completer 命令补全函数
func (t *Terminal) completer(d prompt.Document) []prompt.Suggest {
	if d.LineCount() > 1 {
		return nil
	}

	text := d.TextBeforeCursor()
	words := strings.Split(text, " ")

	// 如果是第一个词，提供命令补全
	if len(words) <= 1 {
		return prompt.FilterHasPrefix(t.suggestions, d.GetWordBeforeCursor(), true)
	}

	// 后续可以根据不同命令提供不同的参数补全
	return nil
}

// executor 命令执行函数
func (t *Terminal) executor(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	// 处理退出命令
	if input == "exit" {
		t.exitHandler()
		return
	}

	// 解析命令和参数
	parts := strings.SplitN(input, " ", 2)
	cmd := strings.ToLower(parts[0])

	// 查找命令处理器
	handler, exists := t.commandHandlers[cmd]
	if !exists {
		fmt.Printf("未知命令: %s\n", cmd)
		return
	}

	// 解析参数
	var args []string
	if len(parts) > 1 && parts[1] != "" {
		args = []string{parts[1]}
	}

	// 执行命令
	err := handler(args)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
	}
}

// Run 运行终端界面
func (t *Terminal) Run() {
	p := prompt.New(
		t.executor,
		t.completer,
		prompt.OptionTitle("Go P2P"),
		prompt.OptionPrefix(t.promptPrefix),
		prompt.OptionInputTextColor(prompt.Yellow),
		prompt.OptionPrefixTextColor(prompt.Blue),
		prompt.OptionMaxSuggestion(8),
	)

	p.Run()
}

// PrintInfo 打印信息
func (t *Terminal) PrintInfo(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

// PrintError 打印错误
func (t *Terminal) PrintError(format string, args ...interface{}) {
	fmt.Printf("[ERROR] "+format+"\n", args...)
}

// PrintSuccess 打印成功信息
func (t *Terminal) PrintSuccess(format string, args ...interface{}) {
	fmt.Printf("[SUCCESS] "+format+"\n", args...)
}

// PrintWarning 打印警告信息
func (t *Terminal) PrintWarning(format string, args ...interface{}) {
	fmt.Printf("[WARNING] "+format+"\n", args...)
}

// PrintPeerMessage 打印对等节点消息
func (t *Terminal) PrintPeerMessage(peerName, message string) {
	fmt.Printf("[%s] %s\n", peerName, message)
}