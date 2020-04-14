package api

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"time"

	"github.com/Shopify/sarama"
	"github.com/bsm/sarama-cluster" //support automatic consumer-group rebalancing and offset tracking
	"github.com/gwaylib/log"

	//"github.com/sdbaiguanghe/glog"
	cfg "github.com/filecoin-project/lotus/api/config"
)

const (
	kafkaUser   = "hlmkafka"
	kafkaPasswd = "HLMkafka2019"
)

var (
	kafkaCertDir = cfg.GetRootDir() + "/api/config/kafka-cert"
)

//生产消息模式
func KafkaProducer(producerData string, topic string) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Timeout = 5 * time.Second
	config.Net.SASL.Enable = true
	config.Net.SASL.Handshake = true
	config.Net.SASL.User = kafkaUser
	config.Net.SASL.Password = kafkaPasswd
	//证书位置
	kafkaCert := kafkaCertDir
	certBytes, err := ioutil.ReadFile(kafkaCert)
	if err != nil {
		log.Warnf(err.Error())
	}
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		panic("kafka producer failed to parse root certificate")
	}
	config.Net.TLS.Config = &tls.Config{
		//Certificates:       []tls.Certificate{},
		RootCAs:            clientCertPool,
		InsecureSkipVerify: true,
	}

	config.Net.TLS.Enable = true
	cfgs := cfg.GetConf()
	address := cfgs.ApiReport
	p, err := sarama.NewSyncProducer(address, config)
	if err != nil {
		log.Warnf("sarama.NewSyncProducer err, message=%s \n", err)
		return
	}
	defer p.Close()
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(producerData),
	}
	part, offset, err := p.SendMessage(msg)
	if err != nil {
		log.Warn("send message(%s) err=%v \n", producerData, err)
	} else {
		log.Infof("发送成功，partition=%d, offset=%d \n", part, offset)
	}

}

func KafkaConsumer(groupID string, topics []string) []byte {
	config := cluster.NewConfig()
	config.Consumer.Return.Errors = true
	config.Group.Return.Notifications = true
	config.Net.SASL.Enable = true
	config.Net.SASL.User = kafkaUser
	config.Net.SASL.Password = kafkaPasswd
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	//证书位置
	kafkaCert := kafkaCertDir
	certBytes, err := ioutil.ReadFile(kafkaCert)
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		panic("kafka producer failed to parse root certificate")
	}

	config.Net.TLS.Config = &tls.Config{
		//Certificates:       []tls.Certificate{},
		RootCAs:            clientCertPool,
		InsecureSkipVerify: true,
	}

	config.Net.TLS.Enable = true
	cfgs := cfg.GetConf()
	address := cfgs.ApiReport
	// init consumer
	consumer, err := cluster.NewConsumer(address, groupID, topics, config)
	if err != nil {
		panic(err)
	}
	defer consumer.Close()
	// trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	// consume errors
	go func() {
		for err := range consumer.Errors() {
			log.Printf("Error: %s\n", err.Error())
		}
	}()
	// consume notifications
	go func() {
		for ntf := range consumer.Notifications() {
			log.Printf("Rebalanced: %+v\n", ntf)
		}
	}()
	// consume messages, watch signals
	select {
	case msg, ok := <-consumer.Messages():
		if ok {
			fmt.Fprintf(os.Stdout, "%s/%d/%d\t%s\t%s\n", msg.Topic, msg.Partition, msg.Offset, msg.Key, msg.Value)
			consumer.MarkOffset(msg, "") // mark message as processed
			return msg.Value
		}
	case <-signals:
		return nil
	}
	return nil
}
