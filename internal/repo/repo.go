// Package repo 把"任务存取"包在接口后面 —— 这就是 Repository 模式。
//
// 为什么要这层:handler / worker 只该说"给我建个任务、把结果写回去",
// 不该知道底下用的是 GORM 还是裸 SQL。好处和 cache.Cache 接口如出一辙:
//  1. 换实现零改动调用方 —— 单测塞个内存 fake 就不用真库(worker_test 在用)
//  2. 存储细节全部关在这一个文件里,哪天换库/换 ORM,翻这里就行
//
// ORM 用 GORM:不再手写 SQL。这张表只有五条操作,GORM 的收益是
// "建表(AutoMigrate)、NULL 处理、时间戳自动填充"这些样板代码全免掉;
// 代价是多一层反射和隐式行为,所以 Task 结构体上的 gorm 标签就是
// "表结构唯一真相源" —— 改列先改这里。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNotFound 查询的任务不存在。调用方据此决定回 404 还是 500。
var ErrNotFound = errors.New("task not found")

// 任务状态的三种取值。故意只有三态、没有 running:
// 对轮询方来说"排队中"和"计算中"都是"还没好",多一个状态多一次 UPDATE,
// 当前规模不值得(已知简化,不是遗漏)。
const (
	StatusQueued = "queued"
	StatusDone   = "done"
	StatusFailed = "failed"
)

// Task 一次异步规划任务的完整档案 —— 同时是业务结构和 GORM 表模型
// (教学规模只有一张表,不搞 PO/DO 两层转换;等真出现"两边字段对不上"
// 的那天再拆,才是拆的正确时机)。
//
// 表结构唯一真相源就是这些 gorm 标签:
//   - status 用 ENUM,数据库层面就挡住非法状态
//   - (status, created_at) 复合索引:将来"按状态翻任务列表"会用到
//   - CreatedAt/UpdatedAt 是 GORM 的约定字段名,建行/改行时自动填充,
//     不用我们手动 SET
type Task struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	ReqJSON    []byte    `gorm:"column:req_json;type:json;not null"`
	Status     string    `gorm:"column:status;type:enum('queued','done','failed');default:queued;index:idx_status_created,priority:1"`
	ResultJSON []byte    `gorm:"column:result_json;type:json"`
	Error      string    `gorm:"column:error;type:text"`
	CreatedAt  time.Time `gorm:"column:created_at;index:idx_status_created,priority:2"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (Task) TableName() string { return "tasks" }

// TaskRepo 任务档案的存取接口。所有存储细节都藏在实现里,调用方只见接口。
type TaskRepo interface {
	// Create 新建一条 queued 任务,返回自增 id(也就是前端拿到的 plan_id)
	Create(ctx context.Context, reqJSON []byte) (int64, error)
	// Get 按 id 查档案;不存在返回 ErrNotFound
	Get(ctx context.Context, id int64) (Task, error)
	// MarkDone 解算成功:写回结果 JSON
	MarkDone(ctx context.Context, id int64, resultJSON []byte) error
	// MarkFailed 解算失败:写回人类可读的原因(会一路透给前端)
	MarkFailed(ctx context.Context, id int64, errMsg string) error
}

// GormTaskRepo 基于 GORM 的实现。
type GormTaskRepo struct {
	db *gorm.DB
}

// NewGorm 建立 MySQL 连接并返回仓库。
// DSN 形如 user:pass@tcp(host:3306)/dbname?charset=utf8mb4&parseTime=True。
// parseTime=True 是必须的:没有它 MySQL 的 DATETIME 不会转成 time.Time。
func NewGorm(dsn string) (*GormTaskRepo, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// GORM 默认日志在错误时才打,慢查询阈值保持默认 ——
		// 教学规模不需要自定义 logger,但要知道这个东西存在
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	// 连接池参数按"解算任务量"给保守值:任务本来就串行消费,
	// 池子开大了除了多占 MySQL 连接没有任何收益
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return &GormTaskRepo{db: db}, nil
}

// Migrate 建表(已存在则比对差异做增量调整)。启动时跑一次。
// AutoMigrate 是 GORM 的建表/改表方案:教学规模一条调用替代迁移工具,
// 表结构的真相源就是 Task 结构体的 gorm 标签。
func (r *GormTaskRepo) Migrate(ctx context.Context) error {
	if err := r.db.WithContext(ctx).AutoMigrate(&Task{}); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func (r *GormTaskRepo) Create(ctx context.Context, reqJSON []byte) (int64, error) {
	t := Task{ReqJSON: reqJSON, Status: StatusQueued}
	// WithContext:让这条 SQL 跟随请求的取消/超时信号 ——
	// 客户端断开了,没发出的查询就不该再发
	if err := r.db.WithContext(ctx).Create(&t).Error; err != nil {
		return 0, fmt.Errorf("insert task: %w", err)
	}
	return t.ID, nil
}

func (r *GormTaskRepo) Get(ctx context.Context, id int64) (Task, error) {
	var t Task
	err := r.db.WithContext(ctx).First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("query task %d: %w", id, err)
	}
	return t, nil
}

func (r *GormTaskRepo) MarkDone(ctx context.Context, id int64, resultJSON []byte) error {
	// Updates 用 map 而不是 struct:struct 会跳过零值字段,空结果会被静默丢弃
	err := r.db.WithContext(ctx).Model(&Task{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": StatusDone, "result_json": resultJSON}).Error
	if err != nil {
		return fmt.Errorf("mark done %d: %w", id, err)
	}
	return nil
}

func (r *GormTaskRepo) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	err := r.db.WithContext(ctx).Model(&Task{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": StatusFailed, "error": errMsg}).Error
	if err != nil {
		return fmt.Errorf("mark failed %d: %w", id, err)
	}
	return nil
}
