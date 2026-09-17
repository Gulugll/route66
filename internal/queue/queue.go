// Package queue 封装 Redis Stream 的"任务传送带":api 往里放任务,worker 从里取任务。
//
// 为什么用 Stream 而不是 List/ pub-sub:
//   - 消费组(XREADGROUP)让多条任务在多个 worker 之间自动分摊,一条只被一个 worker 领走
//   - 领走没 ACK 的任务会进 PEL(pending 列表),是"worker 崩了任务不丢"的地基
//     (本项目当前没实现 PEL 重新认领,见 worker 包的已知简化说明)
//
// 为什么消息里只放 task_id 不放完整请求:队列应该轻,数据库才是事实源。
// 任务详情在 tasks 表里,worker 拿 id 去查 —— 万一消息体有大小限制或被重放,
// 也不会有"队列和库里的请求对不上"的问题。
package queue

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Queue 一个"流 + 消费组"的组合。stream 名和组名由装配层定,consumer 名
// 在同一个组里必须唯一(它是消费组内"这个 worker"的身份)。
type Queue struct {
	rdb      *redis.Client
	stream   string
	group    string
	consumer string
}

// New 建一个队列并自持 Redis 连接。
// 为什么不复用 cache 包里的连接:那把连接藏在 cache 实现内部,属于它的实现细节;
// 队列自己持一条连接,两边职责清楚,互不牵连 —— 多一条连接的成本可以忽略。
// consumer 传空则用"主机名-进程号"自动生成(同组内必须唯一)。
func New(addr, stream, group, consumer string) *Queue {
	if consumer == "" {
		consumer = consumerName()
	}
	return &Queue{
		rdb:      redis.NewClient(&redis.Options{Addr: addr}),
		stream:   stream,
		group:    group,
		consumer: consumer,
	}
}

// consumerName 给 consumer 一个默认身份:主机名 + 进程号。
// 同一组里重名会互相"抢戏"(消息分给同名者算同一个消费者),所以尽量唯一。
func consumerName() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// Close 关闭自持的 Redis 连接(测试清理 / 进程退出前调用)
func (q *Queue) Close() error {
	return q.rdb.Close()
}

// EnsureGroup 创建消费组(流不存在时顺带建流,MKSTREAM)。
// 启动时跑一次;组已存在是正常情况(BUSYGROUP),不算错误。
func (q *Queue) EnsureGroup(ctx context.Context) error {
	err := q.rdb.XGroupCreateMkStream(ctx, q.stream, q.group, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

// Add 放一条任务进队列。消息里只带 task_id:任务详情在 tasks 表里,
// worker 拿 id 去查 —— 队列应该轻,DB 才是事实源。
func (q *Queue) Add(ctx context.Context, taskID int64) error {
	err := q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]any{
			"task_id": taskID,
		},
	}).Err()
	if err != nil {
		return fmt.Errorf("xadd task %d: %w", taskID, err)
	}
	return nil
}

// Read 从队列拉一条任务(BLOCK 5 秒:没任务就让内核挂起,和 net/http 的
// Accept 同一个思路 —— 不空转烧 CPU)。没有消息时返回 (nil, nil),调用方继续循环。
func (q *Queue) Read(ctx context.Context) ([]redis.XMessage, error) {
	res, err := q.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.group,
		Consumer: q.consumer,
		Streams:  []string{q.stream, ">"}, // ">" = 只领"从没被领过"的新消息
		Count:    1,
		Block:    5 * time.Second, // 没消息就挂起 5 秒再回来检查 ctx
	}).Result()
	if err == redis.Nil {
		return nil, nil // BLOCK 超时没有消息,不是错误
	}
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	return res[0].Messages, nil
}

// Ack 确认处理完成。没有 ACK 的任务会一直留在 PEL 里 ——
// 所以"处理成功"和"处理失败(任务标 failed)"都要 ACK:
// 失败的任务已经落库留了案底,消息本身没有重放价值。
func (q *Queue) Ack(ctx context.Context, id string) error {
	return q.rdb.XAck(ctx, q.stream, q.group, id).Err()
}
