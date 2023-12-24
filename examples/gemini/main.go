package main

import (
	"fmt"
	"github.com/bincooo/chatgpt-adapter"
	"github.com/bincooo/chatgpt-adapter/store"
	"github.com/bincooo/chatgpt-adapter/types"
	"github.com/bincooo/chatgpt-adapter/vars"
	"github.com/sirupsen/logrus"
	"time"
)

const (
	token  = "AIzaxxx"
	preset = ``
)

var pMessages = make([]map[string]string, 0)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	//logrus.SetLevel(logrus.ErrorLevel)
}

func main() {
	manager := adapter.NewBotManager()
	context := ContextLmt("1008611")
	for {
		fmt.Println("\n\nUser：")
	label:
		var prompt string
		_, err := fmt.Scanln(&prompt)
		if err != nil || prompt == "" {
			goto label
		}

		context.Prompt = prompt
		fmt.Println("Bot：")
		handle(context, manager)
		time.Sleep(time.Second)
		store.CacheMessages("1008611", pMessages)
	}
}

func handle(context types.ConversationContext, manager types.BotManager) {
	manager.Reply(context, func(partialResponse types.PartialResponse) {
		if partialResponse.Message != "" {
			fmt.Print(partialResponse.Message)
		}

		if partialResponse.Error != nil {
			logrus.Error(partialResponse.Error)
			return
		}

		if partialResponse.Status == vars.Closed {
			pMessages = append(pMessages, map[string]string{
				"id":     context.MessageId,
				"author": "user",
				"text":   context.Prompt,
			})
			pMessages = append(pMessages, map[string]string{
				"id":     context.MessageId,
				"author": "bot",
				"text":   partialResponse.Message,
			})
			return
		}
	})
}

func ContextLmt(id string) types.ConversationContext {
	return types.ConversationContext{
		Id:     id,
		Bot:    vars.Gemini,
		Token:  token,
		Preset: preset,
		//Format: "【皮皮虾】: [content]",
		Chain: "replace,cache",
		//AppId: "U05382WAQ1M",
		//BaseURL: "https://edge.zjcs666.icu",
		Proxy:       "http://127.0.0.1:7890",
		Model:       vars.Gemini,
		Temperature: .9,
		//H:     Handle,
	}
}
