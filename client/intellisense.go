package client

import (
	"strings"

	"github.com/c-bata/go-prompt"
)

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
var history []string

func promptCompleter(d prompt.Document) []prompt.Suggest {
	if d.LineCount() > 1 || len(strings.TrimSpace(d.CurrentLineBeforeCursor())) == 0 {
		return nil
	}
	return prompt.FilterHasPrefix(suggestions, d.GetWordBeforeCursor(), true)
}

func promptExitChecker(in string, breakline bool) bool {
	return in == "exit>" && breakline
}

func promptChangeLivePrefix() (string, bool) {
	return livePrefix, true
}

func addHistory(s string) {
	history = append(history, s)
}

func ReadInput() string {
	msg := prompt.Input(livePrefix, promptCompleter, prompt.OptionTitle("go-p2p"),
		prompt.OptionLivePrefix(promptChangeLivePrefix),
		prompt.OptionHistory(history),
		prompt.OptionSetExitCheckerOnInput(promptExitChecker))
	addHistory(msg)
	return msg
}
