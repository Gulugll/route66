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

func (f *fakeAgentModel) Complete(_ context.Context, _ []agent.Message, _ []agent.ToolSpec) (agent.Completion, error) {
	f.calls++
	if f.calls == 1 {
		call := agent.ToolCall{ID: "call-1", Type: "function"}
		call.Function.Name = "no_such_tool"
		call.Function.Arguments = `{}`
		return agent.Completion{Calls: []agent.ToolCall{call}}, nil
	}
	return agent.Completion{Text: "推荐顺序:天安门 → 故宫 → 天坛,全程 4.7 km"}, nil
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
	if len(lines) != 2 {
		t.Fatalf("应有 2 行(step + done),实际 %d: %s", len(lines), resp.Body.String())
	}

	var first struct {
		Type string `json:"type"`
		Step struct {
			Iteration int `json:"iteration"`
		} `json:"step"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("第 1 行不是合法 JSON: %v", err)
	}
	if first.Type != "step" || first.Step.Iteration != 1 {
		t.Errorf("第 1 行应为 step 事件: %s", lines[0])
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
