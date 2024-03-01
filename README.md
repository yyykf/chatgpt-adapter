## V2

### TIPS
非源码自行编译者，请下载release下的V2版

命令执行：
```shell
./linux-server -h

```
参数如下：
```
GPT接口适配器。统一适配接口规范，集成了bing、claude-2，gemini...
项目地址：https://github.com/bincooo/chatgpt-adapter

Usage:
  ChatGPT-Adapter [flags]

Flags:
  -h, --help             help for ChatGPT-Adapter
      --port int         服务端口 port (default 8080)
      --proxies string   本地代理 proxies
  -v, --version          version for ChatGPT-Adapter
```

启动：(国内机器请使用本地代理 `proxies`)
```shell
./linux-server --proxies http://127.0.0.1:7890

```

#### 请求列表

model 列表
```txt
{
    "id":       "claude-2",
    "object":   "model",
    "created":  1686935002,
    "owned_by": "claude-adapter"
},
{
    "id":       "bing",
    "object":   "model",
    "created":  1686935002,
    "owned_by": "bing-adapter"
},
{
    "id":       "coze",
    "object":   "model",
    "created":  1686935002,
    "owned_by": "coze-adapter"
},
{
    "id":       "gemini",
    "object":   "model",
    "created":  1686935002,
    "owned_by": "gemini-adapter"
}
```

completions 对话
```txt
/v1/chat/completions
/v1/object/completions
/proxies/v1/chat/completions
```

```curl
curl -i -X POST \
   -H "Content-Type:application/json" \
   -H "Authorization: xxx" \
   -d \
'{
  "stream": true,
  "model": "coze",
  "messages": [
    {
      "role":    "user",
      "content": "hi"
    }
  ]
}' \
 'http://127.0.0.1:8080/v1/chat/completions'
```


#### Authorization 获取

claude:
> 在 `claude.ai` 官网中登陆，浏览器 `cookies` 中取出 `sessionKey` 的值就是 `Authorization` 参数

bing:
> 在 `www.bing.com` 官网中登陆，浏览器 `cookies` 中取出 `_U` 的值就是 `Authorization` 参数

gemini:
> 在 `ai.google.dev` 中申请，获取 token凭证就是 `Authorization` 参数

coze:
> 在 `www.coze.com` 官网中登陆，浏览器 `cookies` 中取出 `sessionid` 、`msToken` 的值就是 `Authorization` 参数
>
> 格式拼接： 
> 
> ${sessionid}[msToken=${msToken}]
> 
> 例子：
> 
> 3fdb9fb39a9bc013049e4315c5xxx[msToken=xxx]
