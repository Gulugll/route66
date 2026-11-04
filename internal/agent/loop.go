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
//
// 工具请求用户确认(返回 *ConfirmError)时,Run 以 *ConfirmNeeded 暂停返回,
// 调用方保存 Pending 并收集用户答复后调 Resume 续跑。
func (a *Agent) Run(ctx context.Context, msgs []Message) (string, []Step, error) {
	history := []Message{NewSystemMsg(a.SystemPrompt)}
	history = append(history, msgs...)
	return a.runLoop(ctx, history, nil)
}

// Resume 从一次 HITL 暂停恢复:以用户答复执行确认工具,同轮其余调用重跑补齐
// tool 消息(OpenAI 协议要求每个 call 都有对账结果),然后继续循环。
// 可能再次返回 *ConfirmNeeded(链式确认),继续 Resume 即可。
func (a *Agent) Resume(ctx context.Context, p Pending, answer string) (string, []Step, error) {
	var conf Confirmable
	for _, t := range a.Tools {
		if t.Spec().Name != p.Call.Function.Name {
			continue
		}
		if c, ok := t.(Confirmable); ok {
			conf = c
		}
		break
	}
	if conf == nil {
		return "", p.Steps, fmt.Errorf("agent: 工具 %q 不支持确认续跑", p.Call.Function.Name)
	}

	// 同轮其余调用从 history 末条 assistant 消息取回(挂起轮全部无结果)
	var siblings []ToolCall
	if n := len(p.History); n > 0 && len(p.History[n-1].ToolCalls) > 0 {
		siblings = p.History[n-1].ToolCalls
	}

	byName := a.toolIndex()
	history := append([]Message(nil), p.History...)
	steps := p.Steps
	// ponytail: 同轮混合"确认+普通"调用时普通调用重跑(搜索类幂等);
	// 系统提示词约束下一轮一确认是常态,真出现高频重跑再改结果缓存
	for _, call := range siblings {
		if call.ID == p.Call.ID {
			continue
		}
		history = append(history, NewToolMsg(call.ID, a.execTool(ctx, byName, call)))
	}
	out, err := conf.RunConfirmed(ctx, json.RawMessage(p.Call.Function.Arguments), answer)
	if err != nil {
		return "", steps, fmt.Errorf("agent: 确认执行失败: %w", err)
	}
	history = append(history, NewToolMsg(p.Call.ID, out))
	return a.runLoop(ctx, history, steps)
}

// runLoop 循环本体,Run 与 Resume 共用。history 必须以 system 或 user 开头,
// 且最后一条(除本轮待执行外)满足 OpenAI 协议对账:assistant(calls) 后紧跟对应 tool 消息。
func (a *Agent) runLoop(ctx context.Context, history []Message, steps []Step) (string, []Step, error) {
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}
	byName := a.toolIndex()
	specs := make([]ToolSpec, 0, len(a.Tools))
	for _, t := range a.Tools {
		specs = append(specs, t.Spec())
	}

	start := len(steps) + 1
	for i := start; i < start+maxIter; i++ {
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
		var (
			mu         sync.Mutex
			confirmErr *ConfirmError
			confirmIdx = -1
		)
		var wg sync.WaitGroup
		for idx, call := range completion.Calls {
			wg.Add(1)
			go func(idx int, call ToolCall) {
				defer wg.Done()
				out, err := a.execToolErr(ctx, byName, call)
				if ce, ok := err.(*ConfirmError); ok {
					// 一轮多个确认调用时只挂起第一个:其余当作未执行,
					// 恢复时重跑补齐(见 Resume)。系统提示词约束下一轮一确认是常态。
					mu.Lock()
					if confirmErr == nil {
						confirmErr, confirmIdx = ce, idx
					}
					mu.Unlock()
					return
				}
				if err != nil {
					out = "错误: " + err.Error()
				}
				results[idx] = out
			}(idx, call)
		}
		wg.Wait()

		// 确认请求:整轮挂起,不带任何 tool 结果,由调用方走 HITL 恢复流程。
		// 挂起轮同样记入步骤链 —— 用户要能在界面上看到"模型问了什么"。
		if confirmErr != nil {
			step.Results = make([]string, len(completion.Calls))
			for j := range step.Results {
				step.Results[j] = "等待用户确认"
			}
			steps = append(steps, step)
			if a.OnStep != nil {
				a.OnStep(step)
			}
			return "", steps, &ConfirmNeeded{Pending: Pending{
				Call:    completion.Calls[confirmIdx],
				Prompt:  confirmErr.Prompt,
				Options: confirmErr.Options,
				History: history,
				Steps:   steps,
			}}
		}

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

func (a *Agent) toolIndex() map[string]Tool {
	byName := make(map[string]Tool, len(a.Tools))
	for _, t := range a.Tools {
		byName[t.Spec().Name] = t
	}
	return byName
}

// execToolErr 执行单次工具调用并保留错误类型(确认请求需要透传给循环)。
// 未知工具返回错误,由调用方决定转为文本还是其他处理。
func (a *Agent) execToolErr(ctx context.Context, byName map[string]Tool, call ToolCall) (string, error) {
	tool, ok := byName[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("不存在名为 %q 的工具,可用工具: %v", call.Function.Name, toolNames(byName))
	}
	return tool.Run(ctx, json.RawMessage(call.Function.Arguments))
}

// execTool 执行单次工具调用。所有失败(未知工具/参数错误/执行出错)
// 都转为错误文本返回给模型,由模型决定重试或调整;中断循环会剥夺其纠错机会。
func (a *Agent) execTool(ctx context.Context, byName map[string]Tool, call ToolCall) string {
	out, err := a.execToolErr(ctx, byName, call)
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
