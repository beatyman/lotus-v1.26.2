package ffiwrapper

import (
	"context"
	"fmt"
	"testing"
)

func TestPrecommit1(t *testing.T) {
	group, err := GroupCpuCache(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	cpuKeys := GetTaskSetKey(group)
	fmt.Println(cpuKeys)
}
