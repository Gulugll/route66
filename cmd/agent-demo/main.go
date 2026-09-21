// agent-demo:手写 ReAct 循环的演示入口,与主服务互不依赖,跑完即退出。
//
// 运行:
//
//	set -a; source .env; set +a
//	export LLM_BASE_URL=https://api.deepseek.com   # 或其他 OpenAI 兼容地址
//	export LLM_API_KEY=sk-xxx
//	export LLM_MODEL=deepseek-chat
//	go run ./cmd/agent-demo "帮我规划一条从天安门出发,途经故宫再到天坛的驾车路线"
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/amap"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/matrix"
)

func main() {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("缺少 LLM_API_KEY。任选一家 OpenAI 兼容服务(DeepSeek/Kimi/Qwen/Ollama),")
		fmt.Println("设置 LLM_BASE_URL / LLM_API_KEY / LLM_MODEL 三个环境变量后重试。")
		os.Exit(1)
	}
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-flash"
	}

	question := "帮我规划一条从天安门出发,途经故宫再到天坛的驾车路线,并告诉我总距离和顺序。"
	if len(os.Args) > 1 {
		question = os.Args[1]
	}

	// 装配:复用 internal 的业务包,只取 agent 需要的最小集。
	// 未配置 AMAP_KEY 时规划工具走 haversine,agent 会如实说明是直线估算。
	distCache := cache.NewMemory(24 * time.Hour)
	amapClient := amap.NewClient(os.Getenv("AMAP_KEY"), distCache)
	matrixService := matrix.NewWithAmap(amapClient)

	brain := agent.NewOpenAICompatible(baseURL, apiKey, model)
	a := &agent.Agent{
		Model: brain,
		Tools: agent.RouteTools(matrixService, amapClient),
		SystemPrompt: "你是路线规划助手 RouteBot。规则:" +
			"① 凡涉及地点坐标,必须先用 search_place 查询,严禁编造坐标;" +
			"② 规划路线一律用 plan_route 工具,不要自己心算;" +
			"③ 拿到工具结果后用简洁中文回答,引用总里程和访问顺序;" +
			"④ 如果工具返回了降级警告(直线估算),必须如实转告用户。",
		MaxIterations: 8,
		OnStep: func(s agent.Step) {
			fmt.Printf("\n──── 第 %d 轮 ────\n", s.Iteration)
			if s.Text != "" {
				fmt.Printf("模型: %s\n", s.Text)
			}
			for i, c := range s.Calls {
				fmt.Printf("调用工具 %s(%s)\n参数: %s\n结果: %s\n",
					c.Function.Name, c.ID, c.Function.Arguments, oneLine(s.Results[i]))
			}
		},
	}

	// agent 聚合了多轮外部调用,整体需要独立的超时预算。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fmt.Printf("问题: %s\n", question)
	answer, _, err := a.Run(ctx, []agent.Message{agent.NewUserMsg(question)})
	if err != nil {
		fmt.Println("\n失败:", err)
		os.Exit(1)
	}
	fmt.Printf("\n════ 最终答案 ════\n%s\n", answer)
}

// oneLine 将工具的 JSON 结果压成单行,避免日志刷屏。
func oneLine(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			continue
		}
		if s[i] == ' ' && (len(out) == 0 || out[len(out)-1] == ' ') {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
