package client

import (
	"strings"

	"github.com/c-bata/go-prompt"
)

var signalInput = make(chan string)

var suggestions = []prompt.Suggest{
	{Text: "all", Description: "查找所有可连接用户"},
	{Text: "connectto", Description: "连接指定用户"},
	{Text: "allow", Description: "允许连接"},
	{Text: "deny", Description: "拒绝连接"},
	{Text: "msg", Description: "发送消息"},
	{Text: "rename", Description: "更改昵称"},
	{Text: "changetotcp", Description: "切换到TCP"},
	{Text: "file", Description: "文件传输"},
	{Text: "exit", Description: "退出"},
}

var livePrefix = "💬 "

func promptCompleter(d prompt.Document) []prompt.Suggest {
	if d.LineCount() > 1 || len(strings.TrimSpace(d.CurrentLineBeforeCursor())) == 0 {
		return nil
	}
	return prompt.FilterHasPrefix(suggestions, d.GetWordBeforeCursor(), true)
}

func promptExitChecker(in string, breakline bool) bool {
	return in == "exit>" && breakline
}

func promptExecutor(in string) {
	signalInput <- in
}

func changeLivePrefix() (string, bool) {
	return livePrefix, true
}

// 该方案显示问题，暂不启用
func PromptRun() {
	p := prompt.New(
		promptExecutor,
		promptCompleter,
		prompt.OptionTitle("go-p2p"),
		prompt.OptionLivePrefix(changeLivePrefix),
		prompt.OptionSetExitCheckerOnInput(promptExitChecker),
	)
	p.Run()
}

func ReadInput() string {
	return prompt.Input(livePrefix, promptCompleter, prompt.OptionTitle("go-p2p"),
		prompt.OptionLivePrefix(changeLivePrefix),
		prompt.OptionSetExitCheckerOnInput(promptExitChecker))
}
