package api

// agent_handler.go —— POST /agent:把 internal/agent 暴露成对话端点。
//
// 响应为 NDJSON 流(每行一个 JSON 对象),而不是一次性 JSON:
// agent 的 ReAct 循环每完成一轮工具调用就有新进展,流式下发让前端
// 能实时渲染步骤链;NDJSON 比 SSE 简单 —— 不需要事件帧格式,
// fetch 的 ReadableStream 逐行 parse 即可。
//
// 会话状态此前在前端(纯文本历史);HITL 引入后,"待确认的循环状态"
// 由服务端保存(agent_session.go),用户答复后凭 session_id 经 /agent/resume 续跑。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/agent"
)

// agentSystemPrompt 固定在服务端:模型的"人设"是产品决策,不该由前端任意改写。
const agentSystemPrompt = "你是路线规划助手 RouteBot。规则:" +
	"① 凡涉及地点坐标,必须先用 confirm_place 查询并让用户确认,严禁编造坐标;" +
	"同名地点可能歧义时,把候选交给用户挑选;" +
	"② 规划路线前必须用 confirm_mode 询问出行方式,用户没说就不要猜;" +
	"③ 规划一律用 plan_route 工具,不要自己心算;" +
	"④ 拿到工具结果后用简洁中文回答,引用总里程和访问顺序;" +
	"⑤ 如果工具返回了降级警告(直线估算),必须如实转告用户。"

// WithAgentModel 注入模型实现(测试用:塞假模型就不依赖外部 LLM 服务)。
func WithAgentModel(m agent.Model) Option { return func(s *Server) { s.agentModel = m } }

// 地图 MCP 模式白名单:与厂商端点一一对应,空串 = 默认底座(Valhalla+Photon)。
func ParseMapProvider(s string) (string, error) {
	switch s {
	case "", "amap", "baidu", "tencent":
		return s, nil
	default:
		return "", fmt.Errorf("map_provider 无效: %q(支持空串/amap/baidu/tencent)", s)
	}
}

// agentToolResultLimit 前端回填结果的体积上限。
// 轨迹段最坏情况(49 段 × 上千坐标点)在 MB 级,取 2MB 余量。
const agentToolResultLimit = 2 << 20

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
	var req struct {
		Messages    []agentChatMsg `json:"messages"`
		MapProvider string         `json:"map_provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效: " + err.Error()})
		return
	}
	provider, err := ParseMapProvider(req.MapProvider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a, err := s.buildRouteAgent(c, provider)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
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

	ndjsonHeaders(c)
	s.finishAgentRun(c, provider, func() (string, []agent.Step, error) {
		return a.Run(c.Request.Context(), msgs)
	})
}

// ndjsonHeaders 流式响应的公共头。Cache-Control 防中间层缓冲破坏逐行推送。
func ndjsonHeaders(c *gin.Context) {
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-cache")
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
