package api

// agent_session.go —— RouteBot HITL 的服务端会话。
//
// 确认类工具触发暂停时,循环的完整状态(含 tool 消息的 history)必须跨 HTTP 请求保存:
// /agent 首次命中确认 → Pending 存这里,流里发 confirm 事件;
// /agent/resume 凭 session_id 取回,以用户答复续跑。
//
// ponytail: 内存 map + 惰性 TTL 清理,重启丢会话可接受 —— 确认是秒级到分钟级的
// 短暂状态,为此引入 Redis 反而增加一条部署依赖;真出现会话丢失投诉再迁。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/settings"
)

const agentSessionTTL = 15 * time.Minute

type agentSessionEntry struct {
	provider  string // 地图 MCP 模式(""=默认);恢复时按它重建同一工具集
	pending   agent.Pending
	expiresAt time.Time
}

// agentSessions HITL 会话存储。take 即删:Pending 一次性,重复 resume 直接 410,
// 天然防重放(同一确认回答两次会得到两个不同方案,语义是错的)。
type agentSessions struct {
	mu sync.Mutex
	m  map[string]agentSessionEntry
}

func newAgentSessions() *agentSessions {
	return &agentSessions{m: make(map[string]agentSessionEntry)}
}

func (s *agentSessions) put(provider string, p agent.Pending) string {
	s.gc()
	id := s.newID()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = agentSessionEntry{provider: provider, pending: p, expiresAt: time.Now().Add(agentSessionTTL)}
	return id
}

func (s *agentSessions) take(id string) (string, agent.Pending, bool) {
	s.gc()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[id]
	if !ok {
		return "", agent.Pending{}, false
	}
	delete(s.m, id)
	return e.provider, e.pending, true
}

func (s *agentSessions) newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 随机源失败极罕见;回退到时间纳秒仍满足"不可猜"以外的可用性
		return hex.EncodeToString([]byte(time.Now().String()))[:32]
	}
	return hex.EncodeToString(buf)
}

// gc 惰性清理过期会话:put/take 顺手扫一遍,O(n) 但 n 是活跃确认数,个位数。
func (s *agentSessions) gc() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.m {
		if now.After(e.expiresAt) {
			delete(s.m, id)
		}
	}
}

// buildRouteAgent 组装 RouteBot。/agent 与 /agent/resume、/agent/tool_result
// 必须用同一配置(同一套工具与人设)——provider 存会话,恢复时按存储值重建,
// 否则确认/回填前后的工具集不一致会直接改变行为。
// 返回 error 表示 LLM 未配置,调用方回 503。
func (s *Server) buildRouteAgent(c *gin.Context, provider string) (*agent.Agent, error) {
	ctx := c.Request.Context()
	apiKey := s.settingOr(ctx, settings.KeyLLMAPIKey)
	if apiKey == "" {
		return nil, errors.New("未配置 LLM_API_KEY,智能助手不可用(在 .env 或管理台配置 LLM_API_KEY 后重启)")
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
	model := s.agentModel
	if model == nil {
		model = agent.NewOpenAICompatible(baseURL, apiKey, modelName)
	}
	prompt := agentSystemPrompt
	tools := agent.RouteTools(s.matrix, s.src, s.searcher)
	if provider != "" {
		// MCP 模式:检索与规划委托前端直连厂商执行(工具集换成委托执行型)
		tools = agent.MCPRouteTools(s.searcher)
		prompt += "用户已绑定地图服务(" + provider + " 的 MCP 模式):" +
			"search_place 与 plan_route 由用户设备直连地图服务执行并自动回填;" +
			"引用结果时如实转告降级警告。"
	}
	return &agent.Agent{
		Model:         model,
		Tools:         tools,
		SystemPrompt:  prompt,
		MaxIterations: 8,
		// OnStep 在 Run 的同一 goroutine 里被调用,直接写响应流是安全的;
		// 客户端断开后 Write 静默失败,循环跑完自然退出。
		OnStep: func(step agent.Step) {
			writeNDJSON(c, gin.H{"type": "step", "step": step})
		},
		// 正文增量 → delta 事件;推理模型的思考过程 → think 事件,
		// 前端据此把"漫长的等待"变成"看得见的思考"。
		OnDelta: func(d agent.Delta) {
			if d.Content != "" {
				writeNDJSON(c, gin.H{"type": "delta", "text": d.Content})
			}
			if d.Reasoning != "" {
				writeNDJSON(c, gin.H{"type": "think", "text": d.Reasoning})
			}
		},
	}, nil
}

// finishAgentRun 统一处理循环的三种结局:
// 确认请求 → 存会话发 confirm 事件;前端委托执行 → 存会话发 tool_exec 事件;
// 错误 → error 事件;正常 → done 事件。
// run 闭包装着 Run / Resume / ResumeExec,三者结局形状相同。
func (s *Server) finishAgentRun(c *gin.Context, provider string, run func() (string, []agent.Step, error)) {
	answer, _, err := run()
	var cn *agent.ConfirmNeeded
	if errors.As(err, &cn) {
		sid := s.sessions.put(provider, cn.Pending)
		writeNDJSON(c, gin.H{
			"type":       "confirm",
			"session_id": sid,
			"prompt":     cn.Pending.Prompt,
			"options":    cn.Pending.Options,
		})
		return
	}
	var ex *agent.ExecSuspended
	if errors.As(err, &ex) {
		sid := s.sessions.put(provider, ex.Pending)
		task := ex.Pending.Task
		writeNDJSON(c, gin.H{
			"type":       "tool_exec",
			"session_id": sid,
			"phase":      task.Phase,
			"tool":       ex.Pending.Call.Function.Name,
			"params":     task.Params,
			"hint":       task.Hint,
		})
		return
	}
	if err != nil {
		writeNDJSON(c, gin.H{"type": "error", "error": err.Error()})
		return
	}
	writeNDJSON(c, gin.H{"type": "done", "answer": answer})
}

// agentResume POST /agent/resume:用户在确认卡片上答复后续跑循环。
func (s *Server) agentResume(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
		Answer    string `json:"answer"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效: " + err.Error()})
		return
	}
	if req.SessionID == "" || req.Answer == "" || len(req.Answer) > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 与 answer(≤300 字)必填"})
		return
	}
	provider, pending, ok := s.sessions.take(req.SessionID)
	if !ok {
		c.JSON(http.StatusGone, gin.H{"error": "确认已过期或不存在,请重新提问"})
		return
	}
	a, err := s.buildRouteAgent(c, provider)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	ndjsonHeaders(c)
	s.finishAgentRun(c, provider, func() (string, []agent.Step, error) {
		return a.Resume(c.Request.Context(), pending, req.Answer)
	})
}

// agentToolResult POST /agent/tool_result:前端直连地图 MCP 执行完毕后回填续跑。
// provider 从会话取 —— 恢复时按首次请求的配置重建,不信任回填请求。
func (s *Server) agentToolResult(c *gin.Context) {
	var req struct {
		SessionID string          `json:"session_id"`
		Result    json.RawMessage `json:"result"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效: " + err.Error()})
		return
	}
	if req.SessionID == "" || len(req.Result) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 与 result 必填"})
		return
	}
	if len(req.Result) > agentToolResultLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "result 超过体积上限"})
		return
	}
	provider, pending, ok := s.sessions.take(req.SessionID)
	if !ok {
		c.JSON(http.StatusGone, gin.H{"error": "执行会话已过期或不存在,请重新提问"})
		return
	}
	if pending.Kind != "exec" {
		c.JSON(http.StatusConflict, gin.H{"error": "该会话不是前端执行任务,请走 /agent/resume"})
		return
	}
	a, err := s.buildRouteAgent(c, provider)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	ndjsonHeaders(c)
	s.finishAgentRun(c, provider, func() (string, []agent.Step, error) {
		return a.ResumeExec(c.Request.Context(), pending, string(req.Result))
	})
}
