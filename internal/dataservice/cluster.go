package dataservice

import (
	"log"
	"sort"
	"sync"

	"github.com/zilliztech/milvus-distributed/internal/proto/commonpb"

	"github.com/zilliztech/milvus-distributed/internal/proto/datapb"

	"github.com/zilliztech/milvus-distributed/internal/proto/internalpb2"
)

type (
	dataNode struct {
		id      int64
		address struct {
			ip   string
			port int64
		}
		client     DataNodeClient
		channelNum int
	}
	dataNodeCluster struct {
		mu       sync.RWMutex
		finishCh chan struct{}
		nodes    []*dataNode
	}
)

func newDataNodeCluster(finishCh chan struct{}) *dataNodeCluster {
	return &dataNodeCluster{
		finishCh: finishCh,
		nodes:    make([]*dataNode, 0),
	}
}

func (c *dataNodeCluster) Register(dataNode *dataNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checkDataNodeNotExist(dataNode.address.ip, dataNode.address.port) {
		c.nodes = append(c.nodes, dataNode)
		if len(c.nodes) == Params.DataNodeNum {
			close(c.finishCh)
		}
	}
}

func (c *dataNodeCluster) checkDataNodeNotExist(ip string, port int64) bool {
	for _, node := range c.nodes {
		if node.address.ip == ip || node.address.port == port {
			return false
		}
	}
	return true
}

func (c *dataNodeCluster) GetNumOfNodes() int {
	return len(c.nodes)
}

func (c *dataNodeCluster) GetNodeIDs() []int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ret := make([]int64, len(c.nodes))
	for _, node := range c.nodes {
		ret = append(ret, node.id)
	}
	return ret
}

func (c *dataNodeCluster) WatchInsertChannels(groups []channelGroup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sort.Slice(c.nodes, func(i, j int) bool { return c.nodes[i].channelNum < c.nodes[j].channelNum })
	for i, group := range groups {
		_, err := c.nodes[i%len(c.nodes)].client.WatchDmChannels(&datapb.WatchDmChannelRequest{
			Base: &commonpb.MsgBase{
				MsgType:   commonpb.MsgType_kDescribeCollection,
				MsgID:     -1, // todo
				Timestamp: 0,  // todo
				SourceID:  Params.NodeID,
			},
			ChannelNames: group,
		})
		if err != nil {
			log.Println(err.Error())
			continue
		}
	}
}

func (c *dataNodeCluster) GetDataNodeStates() ([]*internalpb2.ComponentInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ret := make([]*internalpb2.ComponentInfo, 0)
	for _, node := range c.nodes {
		states, err := node.client.GetComponentStates(&commonpb.Empty{})
		if err != nil {
			log.Println(err.Error())
			continue
		}
		ret = append(ret, states.State)
	}
	return ret, nil
}

func (c *dataNodeCluster) FlushSegment(request *datapb.FlushSegRequest) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, node := range c.nodes {
		if _, err := node.client.FlushSegments(request); err != nil {
			log.Println(err.Error())
			continue
		}
	}
}

func (c *dataNodeCluster) ShutDownClients() {
	for _, node := range c.nodes {
		if err := node.client.Stop(); err != nil {
			log.Println(err.Error())
			continue
		}
	}
}
