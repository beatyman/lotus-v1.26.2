package monitor

import (
	"huangdong2012/filecoin-monitor/metric"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace"
	"sync"
	"time"
)

var (
	once = &sync.Once{}
)

func Init(kind model.PackageKind, minerID string, hostNo string) {
	once.Do(func() {
		opt := &model.BaseOptions{
			PackageKind: kind,
			MinerID:     minerID,
			HostNo:      hostNo,
		}
		trace.Init(opt, &model.TraceOptions{
			ExportAll:   true,
			SpanLogDir:  "",
			SpanLogName: "",
		})
		metric.Init(opt, &model.MetricOptions{
			PushUrl:      "", //"http://localhost:9091",
			PushJob:      "monitor-job",
			PushInterval: time.Second * 10,
		})
	})
}
