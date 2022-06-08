package client

import (
	"github.com/c-bata/go-prompt"
)

var signalInput = make(chan string)

func promptCompleter(d prompt.Document) []prompt.Suggest {
	promptSuggest := []prompt.Suggest{
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
	return prompt.FilterHasPrefix(promptSuggest, d.GetWordBeforeCursor(), true)
}

func executor(in string) {
	signalInput <- in
}

func exitChecker(in string, breakline bool) bool {
	return in == "exit>" && breakline
}

func promptRun() {
	p := prompt.New(
		executor,
		promptCompleter,
		prompt.OptionPrefix("💬 "),
		prompt.OptionTitle("go-p2p"),
		prompt.OptionSetExitCheckerOnInput(exitChecker),
	)
	p.Run()
}
