package util

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	cmdtypes "github.com/bincooo/chatgpt-adapter/cmd/types"
	cmdvars "github.com/bincooo/chatgpt-adapter/cmd/vars"
	"github.com/bincooo/chatgpt-adapter/store"
	"github.com/bincooo/chatgpt-adapter/types"
	"github.com/bincooo/chatgpt-adapter/utils"
	"github.com/bincooo/chatgpt-adapter/vars"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
)

func DoGeminiAIComplete(ctx *gin.Context, token string, r *cmdtypes.RequestDTO) {
	conversationMapper := make(map[string]*types.ConversationContext)
	isDone := false
	if token == "" || token == "auto" {
		token = bingAIToken
	}
	fmt.Println("TOKEN_KEY: " + token)

	defer func() {
		for _, conversationContext := range conversationMapper {
			cmdvars.Manager.Remove(conversationContext.Id, conversationContext.Bot)
		}
	}()

	// 重试次数
	retry := 3
label:
	if isDone {
		return
	}

	isClose := false
	retry--

	context, err := createGeminiAIConversation(r, token, func() bool { return isClose })
	if err != nil {
		if retry > 0 {
			logrus.Warn("重试中...")
			goto label
		}
		responseGeminiError(ctx, err, r.Stream, token)
		return
	}

	partialResponse := cmdvars.Manager.Reply(*context, func(response types.PartialResponse) {
		if response.Status == vars.Begin {
			conversationMapper[context.Id] = context
		}
		if r.Stream {
			if response.Status == vars.Begin {
				ctx.Status(200)
				ctx.Header("Accept", "*/*")
				ctx.Header("Content-Type", "text/event-stream")
				ctx.Writer.Flush()
				return
			}

			if response.Error != nil {
				isClose = true
				err = response.Error
				if response.Error.Error() == "resolve timeout" {
					retry = 0
				}
				if retry <= 0 {
					responseGeminiError(ctx, response.Error, r.Stream, token)
				}
				return
			}

			if len(response.Message) > 0 {
				// 正常输出了，舍弃重试
				retry = 0
				select {
				case <-ctx.Request.Context().Done():
					isClose = true
					isClose = true
				default:
					if !SSEString(ctx, response.Message) {
						isClose = true
						isDone = true
					}
				}
			}

			if response.Status == vars.Closed {
				SSEEnd(ctx)
				isClose = true
			}
		} else {
			select {
			case <-ctx.Request.Context().Done():
				isClose = true
				isDone = true
			default:
			}
		}
	})

	// 发生错误了，重试一次
	if partialResponse.Error != nil && retry > 0 {
		logrus.Warn("重试中...")
		goto label
	}

	// 什么也没有返回，重试一次
	if !isDone && len(partialResponse.Message) == 0 && retry > 0 {
		logrus.Warn("重试中...")
		goto label
	}

	// 非流响应
	if !r.Stream && !isDone {
		if partialResponse.Error != nil {
			responseGeminiError(ctx, partialResponse.Error, r.Stream, token)
			return
		}
		ctx.JSON(200, BuildCompletion(partialResponse.Message))
	}
}

// 构建BingAI的上下文
func createGeminiAIConversation(r *cmdtypes.RequestDTO, token string, IsClose func() bool) (*types.ConversationContext, error) {
	var (
		id      = "Gemini-" + uuid.NewString()
		bot     string
		model   string
		appId   string
		chain   string
		message string
		preset  string
	)
	switch r.Model {
	case "Gemini":
		bot = vars.Gemini
		model = ""
	default:
		return nil, errors.New(cmdvars.I18n("UNKNOWN_MODEL") + "`" + r.Model + "`")
	}

	var messages []store.Kv
	messages, preset = geminiMessageConversion(r)

	for idx := len(messages) - 1; idx >= 0; idx-- {
		item := messages[idx]
		if item["author"] == "user" {
			message = item["text"]
			messages = append(messages[:idx], messages[idx+1:]...)
			break
		}
	}

	description := ""
	if l := len(messages); l > vars.BingMaxMessage-2 {
		mergeMessages := messages[0 : l-(vars.BingMaxMessage-4)]

		for _, item := range mergeMessages {
			switch item["author"] {
			case "user":
				description += "Human：" + item["text"] + "\n\n"
			case "bot":
				description += "Assistant：" + item["text"] + "\n\n"
			}
		}

		latelyMessages := messages[l-(vars.BingMaxMessage-4):]
		latelyMessages[0]["text"] = "请改为从此页面回答。\n[使用此页面的对话作为我们之前的对话记录进行后续交流]\n\n" + latelyMessages[0]["text"]
		messages = append([]store.Kv{
			{
				"author":      "user",
				"description": description,
				"contextType": "WebPage",
				"messageType": "Context",
				"sourceName":  "history.md",
				"sourceUrl":   "file:///tmp/history.md",
				"privacy":     "Internal",
			},
		}, latelyMessages...)
	}

	store.CacheMessages(id, messages)
	if message == "" {
		message = "continue"
	}

	ms := messages
	if len(description) > 0 {
		ms = messages[1:]
	}

	fmt.Println("-----------------------Response-----------------\n",
		"-----------------------「 预设区 」-----------------------\n",
		preset,
		"\n\n\n-----------------------「 history.md 」-----------------------\n",
		description,
		"\n\n\n-----------------------「 对话记录 」-----------------------\n",
		ms,
		"\n\n\n-----------------------「 当前对话 」-----------------------\n",
		message,
		"\n--------------------END-------------------")

	if token == "" {
		token = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return &types.ConversationContext{
		Id:          id,
		Token:       token,
		Preset:      preset,
		Prompt:      message,
		Bot:         bot,
		Model:       model,
		Proxy:       cmdvars.Proxy,
		Temperature: r.Temperature,
		AppId:       appId,
		BaseURL:     bingBaseURL,
		Chain:       chain,
		H:           geminiHandle(IsClose),
	}, nil
}

// BingAI stream 流读取数据转换处理
func geminiHandle(IsClose func() bool) types.CustomCacheHandler {
	return func(rChan any) func(*types.CacheBuffer) error {
		matchers := utils.GlobalMatchers()
		partialResponse := rChan.(*http.Response)

		reader := bufio.NewReader(partialResponse.Body)
		var original []byte
		var textBlock = []byte(`"text": "`)

		return func(self *types.CacheBuffer) error {
			if IsClose() {
				self.Closed = true
				return nil
			}

			line, hm, err := reader.ReadLine()
			original = append(original, line...)
			if hm {
				return nil
			}

			if err == io.EOF {
				self.Closed = true
				self.Cache += utils.ExecMatchers(matchers, "\n      ")
				return nil
			}

			if err != nil {
				self.Closed = true
				logrus.Error(err)
				return err
			}

			if len(original) == 0 {
				return nil
			}

			dst := make([]byte, len(original))
			copy(dst, original)
			original = make([]byte, 0)
			if !bytes.Contains(dst, textBlock) {
				return nil
			}
			index := bytes.Index(dst, textBlock)
			self.Cache += utils.ExecMatchers(matchers, string(dst[index+len(textBlock):len(dst)-1]))
			return nil
		}
	}
}

// openai对接格式转换成Gemini接受格式
func geminiMessageConversion(r *cmdtypes.RequestDTO) ([]store.Kv, string) {
	var messages []store.Kv
	var preset string
	temp := ""
	author := ""

	// 知识库上移
	postRef(r)

	// 遍历归类
	for _, item := range r.Messages {
		role := item["role"]
		if author == role {
			content := item["content"]
			if content == "[Start a new Chat]" {
				continue
			}
			temp += "\n\n" + content
			continue
		}

		if temp != "" {
			switch author {
			case "system":
				if len(messages) == 0 {
					preset = temp
					author = role
					temp = item["content"]
					continue
				}
				fallthrough
			case "user":
				messages = append(messages, store.Kv{
					"author": "user",
					"text":   temp,
				})
			case "assistant":
				messages = append(messages, store.Kv{
					"author": "bot",
					"text":   temp,
				})
			}
		}

		author = role
		temp = item["content"]
	}

	// 最后一次循环的文本
	if temp != "" {
		_author := ""
		if author == "system" || author == "user" {
			_author = "user"
		} else {
			_author = "bot"
		}
		if l := len(messages); l > 0 && messages[l-1]["author"] == _author {
			if strings.Contains(temp, "<rule>") { // 特殊标记特殊处理
				messages[l-1]["text"] = temp + "\n\n" + messages[l-1]["text"]
			} else {
				messages[l-1]["text"] += "\n\n" + temp
			}
		} else {
			switch _author {
			case "user":
				messages = append(messages, store.Kv{
					"author": "user",
					"text":   temp,
				})
			case "bot":
				messages = append(messages, store.Kv{
					"author": "bot",
					"text":   temp,
				})
			}
		}
	}
	return messages, preset
}

func responseGeminiError(ctx *gin.Context, err error, isStream bool, token string) {
	ResponseError(ctx, err.Error(), isStream)
}
