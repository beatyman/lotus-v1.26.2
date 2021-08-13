package monitor

import (
	"github.com/shirou/gopsutil/host"
	"huangdong2012/filecoin-monitor/metric"
	"huangdong2012/filecoin-monitor/model"
	"huangdong2012/filecoin-monitor/trace"
	"sync"
	"time"
)

var (
	once = &sync.Once{}
)

func Init(kind model.PackageKind, minerID string) {
	once.Do(func() {
		opt := &model.BaseOptions{
			PackageKind: kind,
			MinerID:     minerID,
			HostNo:      getHostNo(),
		}
		trace.Init(opt, &model.TraceOptions{
			ExportAll:   false, //todo test self span
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

func getHostNo() string {
	info, err := host.Info()
	if err != nil {
		return ""
	}
	return info.HostID
}
