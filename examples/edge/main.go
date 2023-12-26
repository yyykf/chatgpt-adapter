package main

import (
	"fmt"
	cmdtypes "github.com/bincooo/chatgpt-adapter/cmd/types"
	cmdutil "github.com/bincooo/chatgpt-adapter/cmd/util"
	"github.com/bincooo/edge-api/util"
)

func main() {
	const token = "xxx"
	fmt.Println(len("你"))
	r := cmdtypes.RequestDTO{
		Messages: []map[string]string{
			{
				"role":    "user",
				"content": "你好",
			},
			{
				"role":    "bot",
				"content": "你也好",
			},
			{
				"role":    "user",
				"content": "你好",
			},
			{
				"role":    "bot",
				"content": "你也好",
			},
			{
				"role":    "user",
				"content": "你好",
			},
			{
				"role":    "bot",
				"content": "你也好",
			},
			{
				"role":    "user",
				"content": "<!-- 我是注释 -->abc<@-1>这是插入内容</@-1>defga<regex other=1>a:c</regex>",
			},
		},
	}
	cmdutil.XmlPlot(&r)
	if err := util.SolveCaptcha(token); err != nil {
		fmt.Println(err)
	}
}
