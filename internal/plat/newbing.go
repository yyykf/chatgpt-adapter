package plat

import (
	"context"
	"github.com/bincooo/chatgpt-adapter/store"
	"github.com/bincooo/chatgpt-adapter/types"
	"github.com/bincooo/chatgpt-adapter/utils"
	"github.com/bincooo/chatgpt-adapter/vars"
	"github.com/bincooo/edge-api"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"strings"
)

var (
	KievAuth = ""
	RwBf     = ""
)

type BingBot struct {
	sessions map[string]*edge.Chat
}

func init() {
	err := godotenv.Load()
	if err != nil {
		logrus.Error(err)
	}
	KievAuth = utils.LoadEnvVar("BING_KievAuth", KievAuth)
	RwBf = utils.LoadEnvVar("BING_RwBf", RwBf)
}

func NewBingBot() types.Bot {
	return &BingBot{
		make(map[string]*edge.Chat),
	}
}

func (bot *BingBot) Reply(ctx types.ConversationContext) chan types.PartialResponse {
	var message = make(chan types.PartialResponse)
	go func() {
		defer close(message)
		session, ok := bot.sessions[ctx.Id]
		if !ok {
			options, err := edge.NewDefaultOptions(ctx.Token, ctx.BaseURL)
			if err != nil {
				message <- types.PartialResponse{Error: err}
				return
			}

			options.KievAuth(KievAuth, RwBf)
			options.Model(ctx.Model)
			options.Proxies(ctx.Proxy)
			options.TopicToE(true)

			chat := edge.New(options)
			session = &chat
			bot.sessions[ctx.Id] = session
		}

		timeout, cancel := context.WithTimeout(context.TODO(), Timeout)
		defer cancel()
		messages := store.GetMessages(ctx.Id)
		if ctx.Preset != "" {
			messages = append([]map[string]string{
				{
					"author": "user",
					"text":   ctx.Preset,
				},
				{
					"author": "bot",
					"text":   "明白了，有什么可以帮助你的？",
				},
			}, messages...)
		}

		chatMessages := func() []edge.ChatMessage {
			messageL := len(messages)
			if messageL == 0 {
				return nil
			}
			result := make([]edge.ChatMessage, 0)
			for _, item := range messages {
				message := edge.ChatMessage{}
				for k, v := range item {
					message[k] = v
				}
				result = append(result, message)
			}
			return result
		}

		session.Temperature(ctx.Temperature)
		partialResponse, err := session.Reply(timeout, ctx.Prompt, nil, chatMessages())
		if err != nil {
			message <- types.PartialResponse{Error: err}
			return
		}

		//logrus.Info("[MiaoX] - Bot.Session: ", session.session.ConversationId)
		bot.handle(ctx, partialResponse, message)
	}()
	return message
}

func (bot *BingBot) Remove(id string) bool {
	if session, ok := bot.sessions[id]; ok {
		if deleteHistory {
			go session.Delete()
		}
		delete(bot.sessions, id)
	}
	slice := []string{id}
	for key, _ := range bot.sessions {
		if strings.HasPrefix(id+"$", key) {
			delete(bot.sessions, key)
			slice = append(slice, key)
		}
	}
	logrus.Info("[MiaoX] - Bot.Remove: ", slice)
	return true
}

func (bot *BingBot) handle(ctx types.ConversationContext, partialResponse chan edge.ChatResponse, message chan types.PartialResponse) {
	pos := 0
	var r types.CacheBuffer

	if ctx.H != nil {
		r = types.CacheBuffer{
			H: ctx.H(partialResponse),
		}
	} else {
		r = types.CacheBuffer{
			H: func(self *types.CacheBuffer) error {
				response, ok := <-partialResponse
				if !ok {
					self.Closed = true
					return nil
				}

				if response.Error != nil {
					logrus.Error(response.Error)
					self.Closed = true
					return response.Error
				}

				str := []rune(response.Text)
				length := len(str)
				if pos >= length {
					return nil
				}
				self.Cache += string(str[pos:])
				pos = length
				return nil
			},
		}
	}
	for {
		response := r.Read()
		message <- response
		if response.Status == vars.Closed {
			break
		}
	}
}

// =======
