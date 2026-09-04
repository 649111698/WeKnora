package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// UsageReportRunner 每天本地时间 09:00 为开启了使用报告的空间生成并
// 推送前一天的使用统计。裸 ticker 对齐墙钟（审计清理器的模式）：
// 每分钟检查一次，命中 9 点且当天未跑过则执行；进程在 9 点后启动时
// 补跑当天错过的任务（last_run_date 落在配置里，重启不丢）。
type UsageReportRunner struct {
	svc UsageReportService
	db  *gorm.DB

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

const usageReportRunHour = 9

// NewUsageReportRunner 构造函数（容器注入）。
func NewUsageReportRunner(svc UsageReportService, db *gorm.DB) *UsageReportRunner {
	return &UsageReportRunner{
		svc:    svc,
		db:     db,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start 启动调度循环；启动时若已过 9 点且当天未跑过，先补跑一次。
func (r *UsageReportRunner) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		if now := time.Now(); now.Hour() >= usageReportRunHour {
			go r.tick(context.Background())
		}
		go r.loop()
		logger.Info(ctx, "[UsageReport] runner started (daily 09:00 local)")
	})
}

// Stop 停止调度循环。
func (r *UsageReportRunner) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		<-r.doneCh
	})
}

func (r *UsageReportRunner) loop() {
	defer close(r.doneCh)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			// 每分钟醒来只看 9 点这一小时；同一天重复触发由
			// last_run_date 幂等挡住。
			if time.Now().Hour() == usageReportRunHour {
				r.tick(context.Background())
			}
		}
	}
}

func (r *UsageReportRunner) tick(ctx context.Context) {
	now := time.Now()
	var tenants []types.Tenant
	if err := r.db.WithContext(ctx).
		Where("usage_report_config IS NOT NULL AND usage_report_config->>'enabled' = 'true'").
		Find(&tenants).Error; err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"scope": "usage_report_list_tenants"})
		return
	}
	for i := range tenants {
		tenant := tenants[i]
		cfg := tenant.UsageReportConfig
		if cfg == nil || !cfg.Enabled {
			continue
		}
		today := now.Format("2006-01-02")
		if strings.TrimSpace(cfg.LastRunDate) == today {
			continue
		}
		yesterday := now.AddDate(0, 0, -1)
		if err := r.svc.SendUsageReport(ctx, &tenant, yesterday, true); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"tenant_id": tenant.ID,
				"scope":     "usage_report_push",
			})
			continue
		}
	}
}
