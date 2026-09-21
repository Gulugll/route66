package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Agent = 模型 + 工具集 + 人设规则。循环本身不做决策,决策全部来自模型。
type Agent struct {
	Model Model
	Tools []Tool
	// SystemPrompt 建议包含:角色边界、事实必须来自工具(禁止编造坐标等)、
	// 信息足够时直接给出最终答案。
	SystemPrompt string
	// MaxIterations 限制单次 Run 的最大轮数,防止模型陷入无限工具调用,默认 10。
	MaxIterations int
	// OnStep 每轮结束回调,可用于日志、埋点或过程展示;可为 nil。
	OnStep func(Step)
	// OnDelta 模型流式增量回调(正文/思考),可为 nil;转发给 Model 的流式输出。
	OnDelta func(Delta)
}

// Step 记录一轮循环:模型文本、工具调用及对应结果。
type Step struct {
	Iteration int        `json:"iteration"`
	Text      string     `json:"text"`
	Calls     []ToolCall `json:"calls"`
	Results   []string   `json:"results"` // 与 Calls 一一对应,含错误文本
}

// Run 执行完整的 ReAct 循环,返回最终答案与全部过程记录。
// msgs 为对话历史(仅 user/assistant 纯文本,由调用方组装,最后一条必须是 user);
// 循环不持有跨 Run 状态,历史随参数进出,便于后续持久化或恢复。
func (a *Agent) Run(ctx context.Context, msgs []Message) (string, []Step, error) {
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	// 按名字索引工具;模型请求了不存在的工具时返回错误文本,由模型自行纠正。
	byName := make(map[string]Tool, len(a.Tools))
	specs := make([]ToolSpec, 0, len(a.Tools))
	for _, t := range a.Tools {
		byName[t.Spec().Name] = t
		specs = append(specs, t.Spec())
	}

	history := []Message{NewSystemMsg(a.SystemPrompt)}
	history = append(history, msgs...)

	var steps []Step
	for i := 1; i <= maxIter; i++ {
		completion, err := a.Model.Complete(ctx, history, specs, a.OnDelta)
		if err != nil {
			return "", steps, err
		}

		step := Step{Iteration: i, Text: completion.Text, Calls: completion.Calls}

		// 出口:模型未请求工具,当前文本即最终答案。
		if len(completion.Calls) == 0 {
			steps = append(steps, step)
			return completion.Text, steps, nil
		}

		// 先把调用本身记入历史(role=assistant),服务端才能将 role=tool 结果对回。
		history = append(history, NewAssistantMsg(completion.Text, completion.Calls))

		// 并发执行全部工具调用;结果按下标绑定,与调度顺序无关。
		results := make([]string, len(completion.Calls))
		var wg sync.WaitGroup
		for idx, call := range completion.Calls {
			wg.Add(1)
			go func(idx int, call ToolCall) {
				defer wg.Done()
				results[idx] = a.execTool(ctx, byName, call)
			}(idx, call)
		}
		wg.Wait()
		step.Results = results

		// 结果逐条回填历史,顺序与 Calls 一一对应,ToolCallID 必须一致。
		for j, call := range completion.Calls {
			history = append(history, NewToolMsg(call.ID, results[j]))
		}

		steps = append(steps, step)
		if a.OnStep != nil {
			a.OnStep(step)
		}
	}

	// 超限时返回已有过程,调用方可据此判断模型卡在哪一步。
	return "", steps, fmt.Errorf("agent: 超过最大迭代次数 %d 仍未得出答案(模型可能陷入循环)", maxIter)
}

// execTool 执行单次工具调用。所有失败(未知工具/参数错误/执行出错)
// 都转为错误文本返回给模型,由模型决定重试或调整;中断循环会剥夺其纠错机会。
func (a *Agent) execTool(ctx context.Context, byName map[string]Tool, call ToolCall) string {
	tool, ok := byName[call.Function.Name]
	if !ok {
		return fmt.Sprintf("错误: 不存在名为 %q 的工具,可用工具: %v", call.Function.Name, toolNames(byName))
	}
	out, err := tool.Run(ctx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		return "错误: " + err.Error()
	}
	return out
}

func toolNames(m map[string]Tool) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}
