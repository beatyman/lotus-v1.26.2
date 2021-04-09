package rpcclient

import (
	"github.com/gwaylib/log"
	"net/rpc"
)

//var client =
//var client = NewClient()

type DqueRequest struct {
	Data []byte
}
type DqueResponse struct {
	Success string
}

//func NewClient() *rpc.Client {
//	conn, err := rpc.DialHTTP("tcp", "127.0.0.1:8905")
//	if err != nil {
//		log.Error("NewPeer2PeerDiscovery conn err ---", err)
//	}
//	return conn
//}

func PostRpc(data []byte) error {
	conn, err := rpc.DialHTTP("tcp", "127.0.0.1:8905")

	log.Info("22222222222222222")
	if err != nil {
		log.Error("-----------------------")
		return err
	}
	req := DqueRequest{
		Data: data,
	}
	var res DqueResponse
	//err := conn.Call("DiskQueue.Put", req, &res)
	err = conn.Call("DiskQueue.Put", req, &res)
	//call := xclient.Go("DiskQueue.Put", req, &res, nil)
	//if call.Error != nil {
	if err != nil {
		log.Error("rpc --------------------------Err -------", err)
		return err
	}
	log.Info("33333333333333333333333333----------", res.Success)
	return nil
}

//func Send(data []byte) error {
//	err := PostRpc(data)
//	if err != nil {
//		client.Close()
//		log.Info("reconnect rpc server ...>>>")
//		conn, err := rpc.DialHTTP("tcp", "127.0.0.1:1234")
//		if err != nil {
//			log.Error("reconnect rpc server fault...")
//			return err
//		}
//		client = conn
//	}
//	return nil
//}
