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
	)
	switch r.Model {
	case "Gemini":
		bot = vars.Gemini
		model = ""
	default:
		return nil, errors.New(cmdvars.I18n("UNKNOWN_MODEL") + "`" + r.Model + "`")
	}

	var messages = geminiMessageConversion(r)
	for idx := len(messages) - 1; idx >= 0; idx-- {
		item := messages[idx]
		if item["author"] == "user" {
			message = item["text"]
			messages = append(messages[:idx], messages[idx+1:]...)
			break
		}
	}

	store.CacheMessages(id, messages)
	if message == "" {
		message = "continue"
	}

	fmt.Println("-----------------------Response-----------------\n",
		"\n\n\n-----------------------「 对话记录 」-----------------------\n",
		messages,
		"\n\n\n-----------------------「 当前对话 」-----------------------\n",
		message,
		"\n--------------------END-------------------")

	if token == "" {
		token = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return &types.ConversationContext{
		Id:          id,
		Token:       token,
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

// gemini stream 流读取数据转换处理
func geminiHandle(IsClose func() bool) types.CustomCacheHandler {
	return func(rChan any) func(*types.CacheBuffer) error {
		matchers := utils.GlobalMatchers()
		matchers = append(matchers, &types.StringMatcher{
			Find: `*`,
			H: func(index int, content string) (state int, result string) {
				// 换行符处理
				content = strings.ReplaceAll(content, `\n`, "\n")
				// <符处理
				idx := strings.Index(content, "\\u003c")
				for idx >= 0 {
					content = content[:idx] + "<" + content[idx+6:]
					idx = strings.Index(content, "\\u003c")
				}
				// >符处理
				idx = strings.Index(content, "\\u003e")
				for idx >= 0 {
					content = content[:idx] + ">" + content[idx+6:]
					idx = strings.Index(content, "\\u003e")
				}
				// "符处理
				content = strings.ReplaceAll(content, `\"`, "\"")
				return types.MAT_MATCHED, content
			},
		})

		partialResponse := rChan.(*http.Response)

		reader := bufio.NewReader(partialResponse.Body)
		var original []byte
		var textBlock = []byte(`"text": "`)

		var isError = false
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
				if isError {
					return errors.New(string(original))
				}
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

			if isError {
				return nil
			}

			dst := make([]byte, len(original))
			copy(dst, original)
			if bytes.Contains(dst, []byte(`"error":`)) {
				isError = true
				return nil
			}

			original = make([]byte, 0)
			if !bytes.Contains(dst, textBlock) {
				return nil
			}
			index := bytes.Index(dst, textBlock)
			rawText := string(dst[index+len(textBlock) : len(dst)-1])
			logrus.Info("rawText ---- ", rawText)
			self.Cache += utils.ExecMatchers(matchers, rawText)
			return nil
		}
	}
}

// openai对接格式转换成Gemini接受格式
func geminiMessageConversion(r *cmdtypes.RequestDTO) []store.Kv {
	var messages []store.Kv
	appen := ""
	lastRole := ""

	// 知识库上移
	postRef(r)

	// 遍历归类
	for _, item := range r.Messages {
		role := item["role"]
		if role == "system" {
			role = "user"
		}

		if lastRole == "" {
			lastRole = role
			appen += item["content"]
			continue
		}

		if lastRole == role {
			appen += "\n\n" + item["content"]
			continue
		}

		t := ""
		switch lastRole {
		case "user":
			t = "user"
		case "assistant":
			t = "bot"
		default:
			continue
		}

		messages = append(messages, store.Kv{
			"author": t,
			"text":   appen,
		})
		lastRole = role
		appen = item["content"]
	}

	// 最后一次循环的文本
	if appen != "" {
		t := ""
		switch lastRole {
		case "user":
			t = "user"
		case "assistant":
			t = "bot"
		}
		messages = append(messages, store.Kv{
			"author": t,
			"text":   appen,
		})
		if t == "bot" {
			messages = append(messages, store.Kv{
				"author": "user",
				"text":   "[continue]",
			})
		}
	}
	return messages
}

func responseGeminiError(ctx *gin.Context, err error, isStream bool, token string) {
	ResponseError(ctx, err.Error(), isStream)
}
