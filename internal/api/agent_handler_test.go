package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"awesomeProject/internal/agent"
	"awesomeProject/internal/amap"
	"awesomeProject/internal/cache"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/settings"

	"github.com/gin-gonic/gin"
)

// fakeAgentModel 可编程假模型:第 1 轮发起工具调用,第 2 轮给最终答案。
// 工具名用不存在的 —— execTool 对未知工具返回错误文本,不会触达真实的
// amap/planner 依赖,测试不需要假高德服务器。
type fakeAgentModel struct{ calls int }

func (f *fakeAgentModel) Complete(_ context.Context, _ []agent.Message, _ []agent.ToolSpec, onDelta func(agent.Delta)) (agent.Completion, error) {
	f.calls++
	if f.calls == 1 {
		call := agent.ToolCall{ID: "call-1", Type: "function"}
		call.Function.Name = "no_such_tool"
		call.Function.Arguments = `{}`
		return agent.Completion{Calls: []agent.ToolCall{call}}, nil
	}
	const text = "推荐顺序:天安门 → 故宫 → 天坛,全程 4.7 km"
	onDelta(agent.Delta{Content: text})
	return agent.Completion{Text: text}, nil
}

func newAgentTestRouter(model agent.Model, env map[string]string) *gin.Engine {
	return NewRouter(
		matrix.New(),
		amap.NewClient("", cache.NewMemory(time.Hour)),
		nil, nil,
		WithAgentModel(model),
		WithEnvFallbacks(env),
	)
}

// 未配置 LLM key → 503,且响应体说明去哪配。
func TestAgentChatWithoutKey(t *testing.T) {
	router := newAgentTestRouter(&fakeAgentModel{}, map[string]string{})

	req := httptest.NewRequest(http.MethodPost, "/agent",
		strings.NewReader(`{"messages":[{"role":"user","content":"规划路线"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("应返回 503,实际 %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "LLM_API_KEY") {
		t.Errorf("错误信息应指明缺哪个配置: %s", resp.Body.String())
	}
}

// 正常流:NDJSON 逐行输出,先 step 后 done;最后一条消息必须是 user。
func TestAgentChatStream(t *testing.T) {
	router := newAgentTestRouter(&fakeAgentModel{}, map[string]string{
		settings.KeyLLMAPIKey: "sk-test",
	})

	body := `{"messages":[{"role":"user","content":"规划路线"}]}`
	req := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("应返回 200,实际 %d: %s", resp.Code, resp.Body.String())
	}
	if ct := resp.Header().Get("Content-Type"); !strings.Contains(ct, "ndjson") {
		t.Fatalf("Content-Type 应为 ndjson,实际 %s", ct)
	}

	lines := strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("应有 3 行(step + delta + done),实际 %d: %s", len(lines), resp.Body.String())
	}

	var stepEvt struct {
		Type string `json:"type"`
		Step struct {
			Iteration int `json:"iteration"`
		} `json:"step"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &stepEvt); err != nil {
		t.Fatalf("第 1 行不是合法 JSON: %v", err)
	}
	if stepEvt.Type != "step" || stepEvt.Step.Iteration != 1 {
		t.Errorf("第 1 行应为 step 事件: %s", lines[0])
	}

	var deltaEvt struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &deltaEvt); err != nil {
		t.Fatalf("第 2 行不是合法 JSON: %v", err)
	}
	if deltaEvt.Type != "delta" || !strings.Contains(deltaEvt.Text, "4.7 km") {
		t.Errorf("第 2 行应为 delta 事件: %s", lines[1])
	}

	var last struct {
		Type   string `json:"type"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("末行不是合法 JSON: %v", err)
	}
	if last.Type != "done" || !strings.Contains(last.Answer, "4.7 km") {
		t.Errorf("末行应为 done + 最终答案: %s", lines[len(lines)-1])
	}
}

// 最后一条不是 user → 400(循环的入口语义由服务端守住,不只靠前端)。
func TestAgentChatRejectsNonUserTail(t *testing.T) {
	router := newAgentTestRouter(&fakeAgentModel{}, map[string]string{
		settings.KeyLLMAPIKey: "sk-test",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent",
		strings.NewReader(`{"messages":[{"role":"assistant","content":"你好"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("应返回 400,实际 %d", resp.Code)
	}
}

// hitlModel 脚本:第 1 轮调真实 confirm_mode(HITL 暂停) → 答复后第 2 轮出答案。
type hitlModel struct {
	second string
	calls  int
	seen   [][]agent.Message
}

func (f *hitlModel) Complete(_ context.Context, msgs []agent.Message, _ []agent.ToolSpec, onDelta func(agent.Delta)) (agent.Completion, error) {
	f.seen = append(f.seen, append([]agent.Message(nil), msgs...))
	f.calls++
	if f.calls == 1 {
		call := agent.ToolCall{ID: "call-mode-1", Type: "function"}
		call.Function.Name = "confirm_mode"
		call.Function.Arguments = `{}`
		return agent.Completion{Calls: []agent.ToolCall{call}}, nil
	}
	onDelta(agent.Delta{Content: f.second})
	return agent.Completion{Text: f.second}, nil
}

// 全链契约:首请求 → confirm 事件(带 session_id 与三选项) →
// resume 带答复 → done。恢复后模型历史必须含确认结果(OpenAI 对账)。
func TestAgentConfirmThenResume(t *testing.T) {
	model := &hitlModel{second: "好的,按驾车规划,全程 4.7 km"}
	router := newAgentTestRouter(model, map[string]string{
		settings.KeyLLMAPIKey: "sk-test",
	})

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	resp := post("/agent", `{"messages":[{"role":"user","content":"从天安门到天坛怎么走"}]}`)
	lines := strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	var confirmEvt struct {
		Type      string   `json:"type"`
		SessionID string   `json:"session_id"`
		Prompt    string   `json:"prompt"`
		Options   []string `json:"options"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &confirmEvt); err != nil {
		t.Fatalf("末行解析失败: %v (%s)", err, resp.Body.String())
	}
	if confirmEvt.Type != "confirm" || confirmEvt.SessionID == "" {
		t.Fatalf("末行应为 confirm 事件: %s", resp.Body.String())
	}
	if len(confirmEvt.Options) != 3 || confirmEvt.Prompt == "" {
		t.Errorf("confirm 应带问题与三选项: %+v", confirmEvt)
	}

	// 错误的 session_id → 410(过期/不存在同一文案,不泄露会话存在性)
	resp = post("/agent/resume", `{"session_id":"deadbeef","answer":"driving 驾车"}`)
	if resp.Code != http.StatusGone {
		t.Fatalf("未知会话应 410,实际 %d: %s", resp.Code, resp.Body.String())
	}

	// 正确答复 → 循环续跑至 done
	resp = post("/agent/resume", `{"session_id":"`+confirmEvt.SessionID+`","answer":"driving 驾车"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("resume 应 200,实际 %d: %s", resp.Code, resp.Body.String())
	}
	resumeLines := strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	var done struct {
		Type   string `json:"type"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(resumeLines[len(resumeLines)-1]), &done); err != nil {
		t.Fatalf("resume 末行解析失败: %v", err)
	}
	if done.Type != "done" || !strings.Contains(done.Answer, "4.7 km") {
		t.Errorf("resume 应以 done 收尾: %s", resp.Body.String())
	}

	// 恢复后模型看到的最后历史必须含 role=tool 的确认结果(协议对账)
	last := model.seen[len(model.seen)-1]
	found := false
	for _, m := range last {
		if m.Role == agent.RoleTool && m.ToolCallID == "call-mode-1" &&
			strings.Contains(*m.Content, "driving") {
			found = true
		}
	}
	if !found {
		t.Errorf("恢复历史缺少确认结果: %+v", last)
	}

	// Pending 一次性:同一 session 再 resume → 410
	resp = post("/agent/resume", `{"session_id":"`+confirmEvt.SessionID+`","answer":"driving 驾车"}`)
	if resp.Code != http.StatusGone {
		t.Fatalf("重复 resume 应 410,实际 %d", resp.Code)
	}
}

// —— 地图 MCP 模式(前端委托执行) ——

// mcpModel 脚本:第 1 轮调 plan_route(MCP 模式下委托前端) → 回填后第 2 轮出答案。
type mcpModel struct{ calls int }

func (f *mcpModel) Complete(_ context.Context, _ []agent.Message, _ []agent.ToolSpec, onDelta func(agent.Delta)) (agent.Completion, error) {
	f.calls++
	if f.calls == 1 {
		call := agent.ToolCall{ID: "call-plan-1", Type: "function"}
		call.Function.Name = "plan_route"
		// 2 点非 manual:matrix → legs 两次委托执行
		call.Function.Arguments = `{"points":[{"name":"A","lat":1,"lng":1},{"name":"B","lat":2,"lng":2}],"manual":false}`
		return agent.Completion{Calls: []agent.ToolCall{call}}, nil
	}
	const text = "方案已生成"
	onDelta(agent.Delta{Content: text})
	return agent.Completion{Text: text}, nil
}

// 全链契约:map_provider=amap → tool_exec(matrix) → tool_result 回填 →
// tool_exec(legs,链式,新 session) → tool_result 回填 → done。
func TestAgentMcpToolExecFlow(t *testing.T) {
	router := newAgentTestRouter(&mcpModel{}, map[string]string{
		settings.KeyLLMAPIKey: "sk-test",
	})

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}
	var toolExec struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Phase     string          `json:"phase"`
		Tool      string          `json:"tool"`
		Params    json.RawMessage `json:"params"`
	}

	resp := post("/agent", `{"map_provider":"amap","messages":[{"role":"user","content":"规划"}]}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("应 200,实际 %d: %s", resp.Code, resp.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &toolExec); err != nil {
		t.Fatalf("末行解析失败: %v (%s)", err, resp.Body.String())
	}
	if toolExec.Type != "tool_exec" || toolExec.SessionID == "" || toolExec.Phase != "matrix" || toolExec.Tool != "plan_route" {
		t.Fatalf("末行应为 tool_exec(matrix): %s", resp.Body.String())
	}
	if !strings.Contains(string(toolExec.Params), `"mode":"driving"`) {
		t.Errorf("执行参数应含点列表与方式: %s", toolExec.Params)
	}

	// 未知会话 → 410
	resp = post("/agent/tool_result", `{"session_id":"deadbeef","result":"{}"}`)
	if resp.Code != http.StatusGone {
		t.Fatalf("未知会话应 410,实际 %d", resp.Code)
	}

	// 回填矩阵 → 链式进入 legs 阶段(新 session)
	resp = post("/agent/tool_result",
		`{"session_id":"`+toolExec.SessionID+`","result":{"matrix":[[0,9],[9,0]],"degraded":false,"warnings":[]}}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("tool_result 应 200,实际 %d: %s", resp.Code, resp.Body.String())
	}
	lines = strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	var legsEvt struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		Phase     string `json:"phase"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &legsEvt); err != nil {
		t.Fatalf("回填后末行解析失败: %v (%s)", err, resp.Body.String())
	}
	if legsEvt.Type != "tool_exec" || legsEvt.Phase != "legs" || legsEvt.SessionID == "" || legsEvt.SessionID == toolExec.SessionID {
		t.Fatalf("应链式进入 legs 阶段(新 session): %s", resp.Body.String())
	}

	// 回填轨迹 → done
	resp = post("/agent/tool_result",
		`{"session_id":"`+legsEvt.SessionID+`","result":{"legs":[{"km":9,"duration_min":15,"polyline":[[1,1],[2,2]]}]}}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("tool_result 应 200,实际 %d: %s", resp.Code, resp.Body.String())
	}
	lines = strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	var done struct {
		Type   string `json:"type"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &done); err != nil {
		t.Fatalf("末行解析失败: %v", err)
	}
	if done.Type != "done" || !strings.Contains(done.Answer, "方案已生成") {
		t.Errorf("应以 done 收尾: %s", resp.Body.String())
	}

	// Pending 一次性:同一 session 再回填 → 410
	resp = post("/agent/tool_result", `{"session_id":"`+legsEvt.SessionID+`","result":{}}`)
	if resp.Code != http.StatusGone {
		t.Fatalf("重复回填应 410,实际 %d", resp.Code)
	}
}

// map_provider 白名单:非法值 400,不兜底。
func TestAgentChatRejectsBadProvider(t *testing.T) {
	router := newAgentTestRouter(&mcpModel{}, map[string]string{
		settings.KeyLLMAPIKey: "sk-test",
	})
	req := httptest.NewRequest(http.MethodPost, "/agent",
		strings.NewReader(`{"map_provider":"gaode","messages":[{"role":"user","content":"规划"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("非法 provider 应 400,实际 %d: %s", resp.Code, resp.Body.String())
	}
}
