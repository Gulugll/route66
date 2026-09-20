package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool 是 agent 可调用的工具抽象:Spec 供模型选择,Run 执行。
// 入参为原始 JSON,由各工具自行反序列化为强类型并校验;
// 参数错误以错误返回,循环层不做兜底。
type Tool interface {
	Spec() ToolSpec
	// Run 执行工具,返回值是喂给模型的文本(本项目约定为 JSON)。
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// parseInput 统一反序列化模型给出的参数,错误信息附带原始入参便于排查。
func parseInput(input json.RawMessage, v any) error {
	if err := json.Unmarshal(input, v); err != nil {
		return fmt.Errorf("工具参数不是合法 JSON 或字段类型不对: %w(模型传的是 %s)", err, string(input))
	}
	return nil
}
