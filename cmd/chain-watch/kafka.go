package main

import (
	"github.com/google/uuid"
	"time"
)

var (
	_kafka_address = []string{
		"kf1.grandhelmsman.com:9093",
		"kf2.grandhelmsman.com:9093",
		"kf3.grandhelmsman.com:9093",
	}
)

const (
	kafkaUser   = "hlmkafka"
	kafkaPasswd = "HLMkafka2019"
)

var (
	kafkaCertDir = "/root/hlm-miner" + "/etc/kafka-cert"
)

// 协议共公部分

type KafkaCommon struct {
	KafkaId        string // 协议唯一ID
	KafkaTimestamp int64  // 发送时间
	Type           string // 协议类型
}

func GenKID() string {
	return uuid.New().String()
}
func GenKTimestamp() int64 {
	return time.Now().UnixNano()
}
