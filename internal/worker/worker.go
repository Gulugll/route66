// Package worker 是异步规划的后台解算者:从 Stream 领任务 → 调 planner 解算 → 写回 DB → ACK。
//
// 它和 api 层跑的是同一个 planner.Compute —— 同步接口算出什么,
// 异步任务就算出什么,两条路径永远一致。worker 自己不认识 gin,
// 不碰 HTTP,也不关心任务是谁提的:它只是"拿到请求,产出结果"。
//
// ── 已知简化(刻意的,不是遗漏) ──
//  1. 失败也 ACK:解算失败时任务标 failed 落库,消息直接确认。
//     没做失败重试——高德偶发抖动会把任务打成 failed,用户重提一次即可。
//  2. 没做 PEL 重新认领:worker 处理到一半崩溃时,任务会滞留在 pending 列表,
//     需要人工 XAUTOCLAIM(或将来加定时认领循环)。当前单 worker 规模下,
//     影响仅限"极少数任务永远 queued",可接受。
//  这两条属于可靠性增强,当前规模下不做,避免过度设计。
package worker

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"awesomeProject/internal/amap"
	"awesomeProject/internal/matrix"
	"awesomeProject/internal/model"
	"awesomeProject/internal/planner"
	"awesomeProject/internal/queue"
	"awesomeProject/internal/repo"
)

// Run 阻塞式消费循环,直到 ctx 被取消。装配层用一个 goroutine 跑它。
// 单循环串行消费:解算本身已经很重(几十次高德请求),单个 worker 内部
// 不必再开并发 —— 要提吞吐是"多起几个 worker 进程"的事,不是加 goroutine 的事。
func Run(ctx context.Context, q *queue.Queue, r repo.TaskRepo, m *matrix.Service, am *amap.Client) error {
	// 消费组要先存在才能 XREADGROUP;进程重启时组已在,EnsureGroup 幂等
	if err := q.EnsureGroup(ctx); err != nil {
		return err
	}
	log.Printf("[worker] started, waiting for tasks")

	for {
		// ctx 取消(进程退出)时 Read 里的 BLOCK 也会跟着醒过来
		select {
		case <-ctx.Done():
			log.Printf("[worker] shutting down")
			return ctx.Err()
		default:
		}

		msgs, err := q.Read(ctx)
		if err != nil {
			// Redis 短暂不可用是常态,不应让 worker 退出:记日志后重试
			log.Printf("[worker] read failed (%v), retrying in 1s", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		for _, msg := range msgs {
			processOne(ctx, q, r, m, am, msg.ID, msg.Values)
		}
	}
}

// processOne 处理单条任务:取档案 → 校验 → 解算 → 写库 → ACK。
// 任何一步失败都把任务标 failed(带原因),然后照样 ACK——
// 消息已无重放价值,失败原因记录在数据库里,前端轮询可见。
func processOne(ctx context.Context, q *queue.Queue, r repo.TaskRepo,
	m *matrix.Service, am *amap.Client, msgID string, values map[string]any) {

	// 消息载荷只带 task_id;完整请求从库里取 —— DB 是事实源
	rawID, _ := values["task_id"].(string)
	taskID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		log.Printf("[worker] message %s has bad task_id %q, ack & drop", msgID, rawID)
		_ = q.Ack(ctx, msgID) // 坏消息留在队列里会持续阻塞,只能确认丢弃
		return
	}

	task, err := r.Get(ctx, taskID)
	if err != nil {
		log.Printf("[worker] task %d fetch failed: %v", taskID, err)
		_ = q.Ack(ctx, msgID) // 查都查不到的任务没法处理,ack 掉防止死循环
		return
	}

	// 任务载荷和 HTTP 请求一样不可信:重新走一遍完整校验
	var req planner.PlanRequest
	if err := json.Unmarshal(task.ReqJSON, &req); err != nil {
		failTask(ctx, r, q, msgID, taskID, "任务载荷解析失败: "+err.Error())
		return
	}
	points := append([]model.Point{req.Origin}, req.Destinations...)
	mode, err := planner.ParseMode(req.Mode)
	if err == nil {
		err = planner.Validate(points, mode, req.Segments)
	}
	var result planner.Result
	if err == nil {
		result, err = planner.Compute(points, mode, req.Manual, req.Segments, m, am)
	}
	if err != nil {
		// 失败写库(前端轮询可见原因),消息 ack 掉
		failTask(ctx, r, q, msgID, taskID, err.Error())
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		failTask(ctx, r, q, msgID, taskID, "结果序列化失败: "+err.Error())
		return
	}
	if err := r.MarkDone(ctx, taskID, resultJSON); err != nil {
		// 写库失败时**不 ACK**:消息留在 PEL 里,重启后还有机会重来
		// (当前没有自动认领,但至少不主动销毁重来的机会)
		log.Printf("[worker] task %d mark done failed: %v (message left pending)", taskID, err)
		return
	}
	if err := q.Ack(ctx, msgID); err != nil {
		log.Printf("[worker] task %d ack failed: %v", taskID, err)
		return
	}
	log.Printf("[worker] task %d done: %.1f km, %d points", taskID, result.TotalKm, len(points))
}

// failTask 统一的失败出口:写库标 failed → ack 消息 → 记日志
func failTask(ctx context.Context, r repo.TaskRepo, q *queue.Queue,
	msgID string, taskID int64, reason string) {
	if err := r.MarkFailed(ctx, taskID, reason); err != nil {
		log.Printf("[worker] task %d mark failed failed: %v", taskID, err)
	}
	if err := q.Ack(ctx, msgID); err != nil {
		log.Printf("[worker] task %d ack failed: %v", taskID, err)
	}
	log.Printf("[worker] task %d failed: %s", taskID, reason)
}
