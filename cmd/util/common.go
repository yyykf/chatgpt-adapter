package util

import (
	"encoding/json"
	"fmt"
	cmdtypes "github.com/bincooo/chatgpt-adapter/cmd/types"
	"github.com/bincooo/chatgpt-adapter/cmd/vars"
	"github.com/bincooo/requests"
	"github.com/bincooo/requests/url"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type encoded string

func (e encoded) MarshalJSON() ([]byte, error) {
	return []byte(strconv.QuoteToASCII(string(e))), nil
}

func CleanToken(token string) {
	if token == "auto" {
		vars.GlobalToken = ""
	}
}

type XmlNode struct {
	index   int
	end     int
	tag     string
	t       int
	content string
	attr    map[string]any
	parent  *XmlNode
	child   []*XmlNode
}

type cacheSlice struct {
	// 深度插入, i 是深度索引，v 是插入内容
	deepSlice []map[uint8]string
	// 正则替换, i 替换次数，v 是正则内容
	regexSlice []map[uint8]any
}

const (
	XML_TYPE_S = iota
	XML_TYPE_X
	XML_TYPE_Ig
)

type XmlParser struct {
	// parse(value string) []*XmlNode
	whiteList []string
}

func NewParser(whiteList []string) *XmlParser {
	return &XmlParser{whiteList}
}

// xml解析的简单实现
func (xml XmlParser) Parse(value string) []*XmlNode {
	messageL := len(value)
	if messageL == 0 {
		return nil
	}

	search := func(content string, index int, ch uint8) int {
		contentL := len(content)
		for i := index + 1; i < contentL; i++ {
			if content[i] == ch {
				return i
			}
		}
		return -1
	}

	searchStr := func(content string, index int, s string) int {
		l := len(s)
		contentL := len(content)
		for i := index + 1; i < contentL; i++ {
			if i+l >= contentL {
				return -1
			}
			if content[i:i+l] == s {
				return i
			}
		}
		return -1
	}

	next := func(content string, index int, ch uint8) bool {
		contentL := len(content)
		if index+1 >= contentL {
			return false
		}
		return content[index+1] == ch
	}

	nextStr := func(content string, index int, s string) bool {
		contentL := len(content)
		if index+1+len(s) >= contentL {
			return false
		}
		return content[index+1:index+1+len(s)] == s
	}

	parseAttr := func(slice []string) map[string]any {
		attr := make(map[string]any)
		for _, it := range slice {
			n := search(it, 0, '=')
			if n <= 0 {
				if len(it) > 0 && it != "=" {
					attr[it] = true
				}
				continue
			}

			if n == len(it)-1 {
				continue
			}

			v1, err := strconv.Atoi(it[n+1:])
			if err == nil {
				attr[it[:n]] = v1
				continue
			}

			v2, err := strconv.ParseFloat(it[n+1:], 10)
			if err == nil {
				attr[it[:n]] = v2
				continue
			}

			v3, err := strconv.ParseBool(it[n+1:])
			if err == nil {
				attr[it[:n]] = v3
				continue
			}

			if it[n+1] == '"' && it[len(it)-1] == '"' {
				attr[it[:n]] = it[n+2 : len(it)-1]
			}
		}
		return attr
	}

	content := value
	contentL := len(content)
	type skv struct {
		s []*XmlNode
		p *skv
	}

	slice := &skv{make([]*XmlNode, 0), nil}

	var curr *XmlNode = nil
	for i := 0; i < contentL; i++ {
		if content[i] == '<' { // 开始标记
			// =========================================================
			if next(content, i, '/') { // 结束标记
				n := search(content, i, '>')
				if n == -1 { // 找不到
					if curr == nil {
						break
					}
					curr = nil
					break
				}

				if curr == nil {
					continue
				}

				split := strings.Split(curr.tag, " ")
				if split[0] == content[i+2:n] {
					step := 2 + len(curr.tag)
					curr.t = XML_TYPE_X
					curr.end = n + 1
					curr.content = content[curr.index+step : curr.end-len(split[0])-3]
					// 解析xml参数
					if len(split) > 1 {
						curr.tag = split[0]
						curr.attr = parseAttr(split[1:])
					}
					i = n

					slice.s = append(slice.s, curr)
					curr = curr.parent
					if curr != nil {
						curr.child = slice.s
					}
					if slice.p != nil {
						slice = slice.p
					}

				}

				// =========================================================
			} else if nextStr(content, i, "!--") { // 是否是注释 <!-- xxx -->
				n := searchStr(content, i+3, "-->")
				if n < 0 {
					i += 2
					continue
				}

				slice.s = append(slice.s, &XmlNode{index: i, end: n + 3, content: content[i : n+3], t: XML_TYPE_Ig})
				i = n + 2

				// =========================================================
			} else { // 新的标记
				n := search(content, i, '>')
				if n == -1 {
					break
				}

				tag := content[i+1 : n]
				if !ContainFor(xml.whiteList, func(item string) bool {
					if strings.HasPrefix(item, "reg:") {
						compile := regexp.MustCompile(item[4:])
						return compile.MatchString(tag)
					}
					return item == tag
				}) {
					i = n
					continue
				}
				if curr == nil {
					curr = &XmlNode{index: i, tag: tag, t: XML_TYPE_S}
					//slice.s = append(slice.s, curr)
				} else {
					node := &XmlNode{index: i, tag: tag, t: XML_TYPE_S, parent: curr}
					slice = &skv{make([]*XmlNode, 0), slice}
					//slice.s = append(slice.s, node)
					curr = node
				}
				i = n
			}
		}
	}

	// =========================================================
	return slice.s
}

// 将知识库的内容往上挪
func postRef(r *cmdtypes.RequestDTO) {
	if messageL := len(r.Messages); messageL > 2 {
		pos := 2 // 1次
		// 最多上挪3次对话
		if messageL > 4 {
			pos = 4 // 2次
		}
		if messageL > 6 {
			pos = 6 // 3次
		}

		var slice []string
		content := r.Messages[messageL-1]["content"]
		for {
			start := strings.Index(content, "<postRef>")
			end := strings.Index(content, "</postRef>")
			if start < 0 {
				break
			}
			if start < end {
				slice = append(slice, content[start+9:end])
				content = content[:start] + content[end+10:]
			}
		}
		r.Messages[messageL-1]["content"] = content

		if len(slice) > 0 {
			prefix := "System: "
			if r.Model != "claude-2.1" {
				prefix = ""
			}
			r.Messages = append(r.Messages[:messageL-pos], append([]map[string]string{
				{
					"role":    "user",
					"content": prefix + strings.Join(slice, "\n\n"),
				},
			}, r.Messages[messageL-pos:]...)...)
		}
	}
}

func ResponseError(ctx *gin.Context, err string, isStream bool) {
	logrus.Error(err)
	if isStream {
		if ctx.Writer.Header().Get("Content-Type") == "" {
			ctx.Writer.Header().Set("Content-Type", "text/event-stream")
		}
		marshal, e := json.Marshal(BuildCompletion("Error: " + err))
		if e != nil {
			return
		}
		ctx.String(200, "data: %s\n\ndata: [DONE]", string(marshal))
	} else {
		ctx.JSON(200, BuildCompletion("Error: "+err))
	}
}

func SSEString(ctx *gin.Context, content string) bool {
	if ctx.Writer.Header().Get("Content-Type") == "" {
		ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	}
	completion := BuildCompletion(content)
	marshal, err := json.Marshal(completion)
	if err != nil {
		logrus.Error(err)
		return false
	}
	if _, err = ctx.Writer.Write([]byte("data: " + string(marshal) + "\n\n")); err != nil {
		logrus.Error(err)
		return false
	} else {
		ctx.Writer.Flush()
		return true
	}
}

func SSEEnd(ctx *gin.Context) {
	if ctx.Writer.Header().Get("Content-Type") == "" {
		ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	}
	// 结尾img标签会被吞？？多加几个空格试试
	marshal, _ := json.Marshal(BuildCompletion("  "))
	if _, err := ctx.Writer.Write(append([]byte("data: "), marshal...)); err != nil {
		logrus.Error(err)
	}
	if _, err := ctx.Writer.Write([]byte("\n\ndata: [DONE]")); err != nil {
		logrus.Error(err)
	}
}

func BuildCompletion(message string) gin.H {
	var completion gin.H
	content := gin.H{"content": message, "role": "assistant"}
	completion = gin.H{
		"choices": []gin.H{
			{
				"message": content,
				"delta":   content,
			},
		},
	}
	return completion
}

func handle(content string, msg map[string]string, nodes []*XmlNode, cache *cacheSlice) {
	for _, node := range nodes {
		needChild := true
		// 注释内容删除
		if node.t == XML_TYPE_Ig {
			ctx := content[node.index:node.end]
			msg["content"] = strings.Replace(msg["content"], ctx, "", -1)
		}

		// 自由深度插入
		if node.t == XML_TYPE_X && node.tag[0] == '@' {
			compile, _ := regexp.Compile(`@-*\d+`)
			if compile.MatchString(node.tag) {
				miss := "0"
				if node.attr != nil {
					if it, ok := node.attr["miss"]; ok {
						if v, o := it.(bool); o && v {
							miss = "1"
						}
					}
				}
				cache.deepSlice = append(cache.deepSlice, map[uint8]string{'i': node.tag[1:], 'v': node.content, 'o': miss})
				ctx := content[node.index:node.end]
				msg["content"] = strings.Replace(msg["content"], ctx, "", -1)
				needChild = false
			}
		}

		// 正则替换
		if node.t == XML_TYPE_X && node.tag == "regex" {
			count := 0 // 默认0，替换所有
			if other, ok := node.attr["other"]; ok {
				if idx, k := other.(int); k {
					count = idx
				}
			}
			cache.regexSlice = append(cache.regexSlice, map[uint8]any{'i': count, 'v': node.content})
			ctx := content[node.index:node.end]
			msg["content"] = strings.Replace(msg["content"], ctx, "", -1)
			needChild = false
		}

		if needChild && len(node.child) > 0 {
			handle(content, msg, node.child, cache)
		}
	}
}

// xml标记实现，用于拓展不同平台未实现的编排功能
// notes by:  https://rentry.org/teralomaniac_clewd_ReleaseNotes.
func XmlPlot(r *cmdtypes.RequestDTO) {
	parser := NewParser([]string{"regex", `reg:@-*\d+`})
	// 深度插入, i 是深度索引，v 是插入内容， o 是指令
	deepSlice := make([]map[uint8]string, 0)
	// 正则替换, i 替换次数，v 是正则内容
	regexSlice := make([]map[uint8]any, 0)
	messageL := len(r.Messages)

retry:
	for _, msg := range r.Messages {
		content := msg["content"]
		nodes := parser.Parse(content)
		cache := &cacheSlice{deepSlice, regexSlice}
		handle(content, msg, nodes, cache)
		deepSlice = cache.deepSlice
		regexSlice = cache.regexSlice
	}

	needRetry := false
	// 正则替换的实现
	for _, reg := range regexSlice {
		i := reg['i'].(int)
		split := strings.Split(reg['v'].(string), ":")
		if len(split) < 2 {
			continue
		}
		before := strings.TrimSpace(split[0])
		after := strings.TrimSpace(strings.Join(split[1:], ""))

		if before == "" {
			continue
		}

		compile := regexp.MustCompile(before)
		if i == 0 { // 默认0，替换所有
			for _, msg := range r.Messages {
				content := msg["content"]
				msg["content"] = compile.ReplaceAllString(content, after)
			}
		} else {
			for _, msg := range r.Messages {
				if i <= 0 {
					break
				}
				content := msg["content"]
				for _, match := range compile.FindStringSubmatch(content) {
					content = strings.Replace(content, match, after, -1)
					i--
				}
				msg["content"] = content
			}
		}

		needRetry = true
	}

	if needRetry {
		regexSlice = make([]map[uint8]any, 0)
		goto retry
	}

	// 深度插入的实现
	for _, d := range deepSlice {
		i, _ := strconv.Atoi(d['i'])
		if d['o'] == "1" && messageL-1 < Abs(i) {
			continue
		}

		if i > 0 {
			// 正插
			if messageL-1 >= i {
				r.Messages[i]["content"] += "\n\n" + d['v']
			} else {
				r.Messages[messageL-1]["content"] += "\n\n" + d['v']
			}
		} else {
			// 反插
			if messageL-1 >= -i {
				r.Messages[messageL-1+i]["content"] += "\n\n" + d['v']
			} else {
				r.Messages[0]["content"] += "\n\n" + d['v']
			}
		}
	}
}

// 删除元素
func Remove[T comparable](slice []T, t T) []T {
	return RemoveFor(slice, func(item T) bool {
		return item == t
	})
}

// 自定义条件删除元素
func RemoveFor[T comparable](slice []T, condition func(item T) bool) []T {
	if len(slice) == 0 {
		return slice
	}

	for idx, item := range slice {
		if condition(item) {
			slice = append(slice[:idx], slice[idx+1:]...)
			break
		}
	}
	return slice
}

// 判断切片是否包含子元素
func Contains[T comparable](slice []T, t T) bool {
	return ContainFor(slice, func(item T) bool {
		return item == t
	})
}

// 判断切片是否包含子元素， condition：自定义判断规则
func ContainFor[T comparable](slice []T, condition func(item T) bool) bool {
	if len(slice) == 0 {
		return false
	}

	for idx := 0; idx < len(slice); idx++ {
		if condition(slice[idx]) {
			return true
		}
	}
	return false
}

func TestNetwork(proxy string) {
	req := url.NewRequest()
	req.Timeout = 5 * time.Second
	req.Proxies = proxy
	req.AllowRedirects = false
	response, err := requests.Get("https://claude.ai/login", req)
	if err == nil && response.StatusCode == 200 {
		fmt.Println("🎉🎉🎉 Network success! 🎉🎉🎉")
		req = url.NewRequest()
		req.Timeout = 5 * time.Second
		req.Proxies = proxy
		req.Headers = url.NewHeaders()
		response, err = requests.Get("https://iphw.in0.cc/ip.php", req)
		if err == nil {
			compileRegex := regexp.MustCompile(`\d+\.\d+\.\d+\.\d+`)
			ip := compileRegex.FindStringSubmatch(response.Text)
			if len(ip) > 0 {
				country := ""
				response, err = requests.Get("https://opendata.baidu.com/api.php?query="+ip[0]+"&co=&resource_id=6006&oe=utf8", nil)
				if err == nil {
					obj, e := response.Json()
					if e == nil {
						if status, ok := obj["status"].(string); ok && status == "0" {
							country = obj["data"].([]interface{})[0].(map[string]interface{})["location"].(string)
						}
					}
				}

				fmt.Println(vars.I18n("IP") + ": " + ip[0] + ", " + country)
			}
		}
	} else {
		fmt.Println("🚫🚫🚫 " + vars.I18n("NETWORK_DISCONNECTED") + " 🚫🚫🚫")
	}
}

func LoadEnvVar(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = defaultValue
	}
	return value
}

func LoadEnvInt(key string, defaultValue int) int {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return defaultValue
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		logrus.Error(err)
		os.Exit(-1)
	}
	return result
}

// 取绝对值
func Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
