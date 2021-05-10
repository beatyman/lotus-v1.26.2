package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	hlmclient "github.com/filecoin-project/lotus/cmd/lotus-storage/client"
)

func main() {
	if err := os.MkdirAll("./testing", 0755); err != nil {
		panic(err)
	}
	defer func() {
		fmt.Println("done")
		os.RemoveAll("./testing")
		fmt.Println("exit")
	}()

	ctx := context.TODO()
	fuc := hlmclient.NewFUseClient("127.0.0.1:1331", "32e895c38e92dc26d7f9e933ea398150")
	if err := fuc.Mount(ctx, "./testing"); err != nil {
		panic(err)
	}
	defer fuc.Umount(ctx, "./testing")

	//time.Sleep(1e9)
	//list, err := ioutil.ReadDir("./testing")
	//if err != nil {
	//	panic(err)
	//}
	//
	//fmt.Printf("read dir done:%+v\n", list)

	fmt.Println("[ctrl+c to exit]")
	end := make(chan os.Signal, 2)
	signal.Notify(end, os.Interrupt, os.Kill)
	<-end
}

//import (
//	"fmt"
//	"os"
//
//	"github.com/hanwen/go-fuse/v2/fuse"
//	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
//)
//
//func main() {
//	mountPoint := "./testing"
//	if err := os.MkdirAll(mountPoint, 0755); err != nil {
//		panic(err)
//	}
//	defer func() {
//		fmt.Println("done")
//		os.RemoveAll(mountPoint)
//		fmt.Println("exit")
//	}()
//
//	prefix := "./test"
//	root := NewMemNodeFSRoot(prefix)
//	conn := nodefs.NewFileSystemConnector(root, nil)
//	server, err := fuse.NewServer(conn.RawFS(), mountPoint, &fuse.MountOptions{
//		Debug: true,
//	})
//	if err != nil {
//		fmt.Printf("Mount fail: %v\n", err)
//		os.Exit(1)
//	}
//	fmt.Println("Mounted!")
//	server.Serve()
//}
