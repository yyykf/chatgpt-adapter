package plat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"github.com/bincooo/chatgpt-adapter/store"
	"github.com/bincooo/chatgpt-adapter/types"
	"github.com/bincooo/chatgpt-adapter/vars"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
)

type GeminiBot struct {
}

const GOOGLE_BASE = "https://generativelanguage.googleapis.com"

func (bot GeminiBot) Reply(ctx types.ConversationContext) chan types.PartialResponse {
	var message = make(chan types.PartialResponse)

	response, err := bot.build(ctx)
	if err != nil {
		go func() {
			message <- types.PartialResponse{
				Error:  err,
				Status: vars.Closed,
			}
			close(message)
		}()
		return message
	}

	go bot.resolve(response, message, ctx)

	return message
}

// 构建请求，返回响应
func (GeminiBot) build(ctx types.ConversationContext) (*http.Response, error) {
	var (
		burl = GOOGLE_BASE + "/v1/models/gemini-pro:streamGenerateContent?key="
	)
	if ctx.BaseURL != "" {
		burl = ctx.BaseURL + "/v1/models/gemini-pro:streamGenerateContent?key="
	}

	messages := store.GetMessages(ctx.Id)
	pMessages := make([]map[string]any, 0)
	for _, msg := range messages {
		switch msg["author"] {
		case "user", "system":
			pMessages = append(pMessages, map[string]any{
				"role": "user",
				"parts": []any{
					map[string]string{
						"text": msg["text"],
					},
				},
			})
		case "bot":
			pMessages = append(pMessages, map[string]any{
				"role": "model",
				"parts": []any{
					map[string]string{
						"text": msg["text"],
					},
				},
			})
		}
	}

	pMessages = append(pMessages, map[string]any{
		"role": "user",
		"parts": []any{
			map[string]string{
				"text": ctx.Prompt,
			},
		},
	})

	marshal, err := json.Marshal(map[string]any{
		"contents": pMessages, // [ { role: user, parts: [ { text: 'xxx' } ] } ]
		"generationConfig": map[string]any{
			"topK":            1,
			"topP":            1,
			"temperature":     ctx.Temperature, // 0.8
			"maxOutputTokens": 2048,
		},
		"safetySettings": []map[string]string{
			{
				"category":  "HARM_CATEGORY_HARASSMENT",
				"threshold": "BLOCK_NONE",
			},
			{
				"category":  "HARM_CATEGORY_HATE_SPEECH",
				"threshold": "BLOCK_NONE",
			},
			{
				"category":  "HARM_CATEGORY_SEXUALLY_EXPLICIT",
				"threshold": "BLOCK_NONE",
			},
			{
				"category":  "HARM_CATEGORY_DANGEROUS_CONTENT",
				"threshold": "BLOCK_NONE",
			},
		},
	})
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	request, err := http.NewRequest(http.MethodPost, burl+ctx.Token, bytes.NewReader(marshal))
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	client := http.DefaultClient
	if ctx.Proxy != "" {
		purl, e := url.Parse(ctx.Proxy)
		if e != nil {
			logrus.Error(e)
			return nil, e
		}
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(purl),
			},
		}
	}

	res, err := client.Do(request)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return res, nil
}

func (GeminiBot) Remove(id string) bool {
	return true
}

func (bot GeminiBot) resolve(partialResponse *http.Response, message chan types.PartialResponse, ctx types.ConversationContext) {
	var r types.CacheBuffer
	defer close(message)
	if ctx.H != nil {
		r = types.CacheBuffer{
			H: ctx.H(partialResponse),
		}
	} else {
		reader := bufio.NewReader(partialResponse.Body)
		var original []byte
		var textBlock = []byte(`"text": "`)
		isError := false

		r = types.CacheBuffer{
			H: func(self *types.CacheBuffer) error {
				line, hm, err := reader.ReadLine()
				original = append(original, line...)
				if hm {
					return nil
				}

				if err == io.EOF {
					self.Closed = true
					if isError {
						message <- types.PartialResponse{
							Error: errors.New(string(original)),
						}
					}
					return nil
				}

				if err != nil {
					message <- types.PartialResponse{
						Error: err,
					}
					self.Closed = true
					return nil
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
				if !bytes.Contains(dst, textBlock) {
					return nil
				}

				original = make([]byte, 0)
				index := bytes.Index(dst, textBlock)
				self.Cache += string(dst[index+len(textBlock) : len(dst)-1])
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

func NewGeminiBot() types.Bot {
	return GeminiBot{}
}
