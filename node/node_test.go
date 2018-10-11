package node

import (
	"bvchain/util/log"
	"bvchain/cfg"

	"testing"
	"time"
	"github.com/stretchr/testify/assert"
)

func TestNodeStartStop(t *testing.T) {
	config := cfg.ResetTestRoot("node_test")

	// create & start node
	n, err := NewNode(config, log.TestingLogger())
	assert.NoError(t, err, "expected no err on NewNode")
	err1 := n.Start()
	if err1 != nil {
		t.Error(err1)
	}
	t.Logf("Started node %v", n.adapter.NodeInfo())

	// wait for the node to produce a block
	blockCh := make(chan interface{})
//	err = n.EventBus().Subscribe(context.Background(), "node_test", types.EventQueryNewBlock, blockCh)
//	assert.NoError(t, err)
	select {
	case <-blockCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the node to produce a block")
	}

	// stop the node
	go func() {
		n.Stop()
	}()

	select {
	case <-n.C4Quit():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for shutdown")
	}
}

