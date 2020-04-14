package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster" //support automatic consumer-group rebalancing and offset tracking
	//"github.com/sdbaiguanghe/glog"

	"github.com/gwaylib/errors"
)

const (
	kafkaUser   = "hlmkafka"
	kafkaPasswd = "HLMkafka2019"
)

var (
	kafkaCertDir = "/root/hlm-miner" + "/api/config/kafka-cert"
)

//生产消息模式
func KafkaProducer(producerData string, topic string) error {
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
		return errors.As(err, kafkaCert)
	}
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		return errors.New("kafka producer failed to parse root certificate")
	}
	config.Net.TLS.Config = &tls.Config{
		//Certificates:       []tls.Certificate{},
		RootCAs:            clientCertPool,
		InsecureSkipVerify: true,
	}

	config.Net.TLS.Enable = true
	address := _kafka_address
	p, err := sarama.NewSyncProducer(address, config)
	if err != nil {
		return errors.As(err)
	}
	defer p.Close()
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(producerData),
	}
	part, offset, err := p.SendMessage(msg)
	if err != nil {
		return errors.As(err)
	}

	log.Infof("发送成功，partition=%d, offset=%d \n", part, offset)
	return nil
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
	address := _kafka_address
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
			log.Warnf("Error: %s\n", err.Error())
		}
	}()
	// consume notifications
	go func() {
		for ntf := range consumer.Notifications() {
			log.Warnf("Rebalanced: %+v\n", ntf)
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
