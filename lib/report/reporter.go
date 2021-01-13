package report

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

type Reporter struct {
	lk        sync.Mutex
	lkSurvivalServer        sync.Mutex
	serverUrl string
	survivalServer bool
	reports chan []byte

	ctx     context.Context
	cancel  func()
	running bool
}

func NewReporter(buffer int) *Reporter {
	ctx, cancel := context.WithCancel(context.TODO())
	r := &Reporter{
		reports: make(chan []byte, buffer),

		ctx:    ctx,
		cancel: cancel,
	}

	go r.Run()
	return r
}

func (r *Reporter) send(data []byte) error {
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("%+v,%s", r, string(debug.Stack())))
		}
	}()

	r.lk.Lock()
	defer r.lk.Unlock()

	if len(r.serverUrl) == 0 || !r.GetSurvivalServer(){
		return nil
	}
	resp, err := http.Post(r.serverUrl, "encoding/json", bytes.NewReader(data))
	if err != nil {
		return errors.As(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("server response not success").As(resp.StatusCode, resp.Status)
	}
	return nil
}

func (r *Reporter) Run() {
	r.lk.Lock()
	if r.running {
		r.lk.Unlock()
		return
	}
	r.running = true
	r.lk.Unlock()
	r.SetSurvivalServer(true)
	errBuff := [][]byte{}
	go func() {
		r.runTimerTestingServer()
	}()
	for {
		select {
		case data := <-r.reports:
			if err := r.send(data); err != nil {
				log.Warn(errors.As(err))

				errBuff = append(errBuff, data)
				continue
			} else if len(errBuff) > 0 {
				sendIdx := 0
				for i, d := range errBuff {
					if err := r.send(d); err != nil {
						break
					}
					sendIdx = i
				}
				// send all success, clean buffer
				if sendIdx == len(errBuff)-1 {
					errBuff = [][]byte{}
				} else {
					errBuff = errBuff[sendIdx:]
				}
			}
		case <-r.ctx.Done():
		}
	}
}

func (r *Reporter) runTimerTestingServer() {
	ticker := time.NewTicker(time.Duration(10) * time.Second)
	quit := make(chan bool, 1)

    go func() {
        for {
            select {
            case <-ticker.C:
                //定义超时3s
                if r.serverUrl != "" {
                    client := &http.Client{Timeout: 3 * time.Second}
                    resp, err := client.Get(r.serverUrl)
                    if err != nil {
                        log.Errorf("report error is ", err)
                        //服务不可用
			r.SetSurvivalServer(false)
                    } else {

                        defer resp.Body.Close()
                        if resp.StatusCode == 200 {
			    r.SetSurvivalServer(true)
                        } else {
			    r.SetSurvivalServer(false)
                        }   
                    }   
                }   
            case <-quit:
                ticker.Stop()
            }   
        }   
    }() 

}
func (r *Reporter) SetUrl(url string) {
	r.lk.Lock()
	defer r.lk.Unlock()
	r.serverUrl = url
}

func (r *Reporter) SetSurvivalServer(surServer bool) {
        r.lkSurvivalServer.Lock()
        defer r.lkSurvivalServer.Unlock()
        r.survivalServer = surServer
}

func (r *Reporter) GetSurvivalServer() bool {
	r.lkSurvivalServer.Lock()
        defer r.lkSurvivalServer.Unlock()
	return r.survivalServer
}

func (r *Reporter) Send(data []byte) {
	r.reports <- data
}

func (r *Reporter) Close() error {
	r.lk.Lock()
	r.running = false
	r.lk.Unlock()

	r.cancel()
	return nil
}
