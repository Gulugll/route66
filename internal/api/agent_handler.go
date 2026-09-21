package api

// agent_handler.go —— POST /agent:把 internal/agent 暴露成对话端点。
//
// 响应为 NDJSON 流(每行一个 JSON 对象),而不是一次性 JSON:
// agent 的 ReAct 循环每完成一轮工具调用就有新进展,流式下发让前端
// 能实时渲染步骤链;NDJSON 比 SSE 简单 —— 不需要事件帧格式,
// fetch 的 ReadableStream 逐行 parse 即可。
//
// 会话状态在前端:每次请求带全量对话历史(user/assistant 纯文本),
// 服务端不存任何会话 —— 和循环本身"历史随参数进出"的设计一致。

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/settings"
)

// agentSystemPrompt 固定在服务端:模型的"人设"是产品决策,不该由前端任意改写。
const agentSystemPrompt = "你是路线规划助手 RouteBot。规则:" +
	"① 凡涉及地点坐标,必须先用 search_place 查询,严禁编造坐标;" +
	"② 规划路线一律用 plan_route 工具,不要自己心算;" +
	"③ 拿到工具结果后用简洁中文回答,引用总里程和访问顺序;" +
	"④ 如果工具返回了降级警告(直线估算),必须如实转告用户。"

// WithAgentModel 注入模型实现(测试用:塞假模型就不依赖外部 LLM 服务)。
func WithAgentModel(m agent.Model) Option { return func(s *Server) { s.agentModel = m } }

// settingOr 按"DB(settings 表) > env 兜底"取配置,与 main.go 的
// keyProviderValue 同一优先级;两处都没配返回空串。
func (s *Server) settingOr(ctx context.Context, name string) string {
	if s.settings != nil {
		if v, err := s.settings.Get(ctx, name); err == nil && v != "" {
			return v
		}
	}
	return s.envFallbacks[name]
}

// agentChatMsg 前端提交的对话消息:只收纯文本(user/assistant),
// 工具调用轮由服务端本次 Run 重新产生,历史里的中间过程不可重放也不该重放。
type agentChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Server) agentChat(c *gin.Context) {
	ctx := c.Request.Context()

	apiKey := s.settingOr(ctx, settings.KeyLLMAPIKey)
	if apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "未配置 LLM_API_KEY,智能助手不可用(在 .env 或管理台配置 LLM_API_KEY 后重启)"})
		return
	}
	baseURL := s.settingOr(ctx, settings.KeyLLMBaseURL)
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	modelName := s.settingOr(ctx, settings.KeyLLMModel)
	if modelName == "" {
		// ponytail: 默认值以 DeepSeek 当前在售模型为准(deepseek-chat 已下线),
		// 换厂商时这里给的就是该厂商的通用兜底,建议显式配置
		modelName = "deepseek-flash"
	}

	var req struct {
		Messages []agentChatMsg `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效: " + err.Error()})
		return
	}

	// 校验与整形:只收 user/assistant 纯文本;最后一条必须是 user
	// (循环从它开始工作);数量与长度设上限,防止一次请求被塞成天书。
	if len(req.Messages) == 0 || len(req.Messages) > 40 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages 数量须在 1~40 之间"})
		return
	}
	msgs := make([]agent.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role != string(agent.RoleUser) && m.Role != string(agent.RoleAssistant) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "message.role 只支持 user/assistant"})
			return
		}
		if m.Content == "" || len(m.Content) > 6000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "message.content 为空或超过 6000 字"})
			return
		}
		msgs = append(msgs, agent.Message{Role: agent.Role(m.Role), Content: &m.Content})
	}
	if req.Messages[len(req.Messages)-1].Role != string(agent.RoleUser) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "最后一条消息必须是 user"})
		return
	}

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-cache")
	model := s.agentModel
	if model == nil {
		model = agent.NewOpenAICompatible(baseURL, apiKey, modelName)
	}
	a := &agent.Agent{
		Model:         model,
		Tools:         agent.RouteTools(s.matrix, s.amap),
		SystemPrompt:  agentSystemPrompt,
		MaxIterations: 8,
		// OnStep 在 Run 的同一 goroutine 里被调用,直接写响应流是安全的;
		// 客户端断开后 Write 静默失败,循环跑完自然退出。
		OnStep: func(step agent.Step) {
			writeNDJSON(c, gin.H{"type": "step", "step": step})
		},
	}

	answer, _, err := a.Run(ctx, msgs)
	if err != nil {
		writeNDJSON(c, gin.H{"type": "error", "error": err.Error()})
		return
	}
	writeNDJSON(c, gin.H{"type": "done", "answer": answer})
}

// writeNDJSON 写一行 JSON 并立即刷给客户端 —— 流式渲染的节奏就靠这次 Flush。
func writeNDJSON(c *gin.Context, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = c.Writer.Write(append(b, '\n'))
	c.Writer.Flush()
}
