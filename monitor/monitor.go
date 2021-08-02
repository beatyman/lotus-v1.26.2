package monitor

import (
	"huangdong2012/filecoin-monitor/metric"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace"
	"time"
)

func Init(kind model.PackageKind, minerID string) {
	opt := &model.BaseOptions{PackageKind: kind, MinerID: minerID}
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
}
