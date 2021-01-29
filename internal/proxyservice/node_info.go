package proxyservice

import (
	"context"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"time"

	grpcproxynodeclient "github.com/zilliztech/milvus-distributed/internal/distributed/proxynode/client"

	"github.com/zilliztech/milvus-distributed/internal/errors"

	"github.com/zilliztech/milvus-distributed/internal/proto/proxypb"
)

type NodeInfo struct {
	ip   string
	port int64
}

type NodeClient interface {
	Init() error
	Start() error
	Stop() error

	InvalidateCollectionMetaCache(request *proxypb.InvalidateCollMetaCacheRequest) error
}

type GlobalNodeInfoTable struct {
	mtx             sync.RWMutex
	nodeIDs         []UniqueID
	infos           map[UniqueID]*NodeInfo
	createClientMtx sync.RWMutex
	// lazy creating, so len(clients) <= len(infos)
	clients map[UniqueID]NodeClient
}

func (table *GlobalNodeInfoTable) randomPick() UniqueID {
	rand.Seed(time.Now().UnixNano())
	l := len(table.nodeIDs)
	choice := rand.Intn(l)
	return table.nodeIDs[choice]
}

func (table *GlobalNodeInfoTable) Pick() (*NodeInfo, error) {
	table.mtx.RLock()
	defer table.mtx.RUnlock()

	if len(table.nodeIDs) <= 0 || len(table.infos) <= 0 {
		return nil, errors.New("no available server node")
	}

	id := table.randomPick()
	info, ok := table.infos[id]
	if !ok {
		// though impossible
		return nil, errors.New("fix me, something wrong in pick algorithm")
	}

	return info, nil
}

func (table *GlobalNodeInfoTable) Register(id UniqueID, info *NodeInfo) error {
	table.mtx.Lock()
	defer table.mtx.Unlock()

	_, ok := table.infos[id]
	if !ok {
		table.infos[id] = info
	}

	if !SliceContain(table.nodeIDs, id) {
		table.nodeIDs = append(table.nodeIDs, id)
	}

	return nil
}

func (table *GlobalNodeInfoTable) createClients() error {
	if len(table.clients) == len(table.infos) {
		return nil
	}

	for nodeID, info := range table.infos {
		_, ok := table.clients[nodeID]
		if !ok {
			log.Println(info)
			table.clients[nodeID] = grpcproxynodeclient.NewClient(context.Background(), info.ip+":"+strconv.Itoa(int(info.port)))
			var err error
			err = table.clients[nodeID].Init()
			if err != nil {
				return err
			}
			err = table.clients[nodeID].Start()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (table *GlobalNodeInfoTable) ObtainAllClients() (map[UniqueID]NodeClient, error) {
	table.mtx.RLock()
	defer table.mtx.RUnlock()

	table.createClientMtx.Lock()
	defer table.createClientMtx.Unlock()

	err := table.createClients()

	return table.clients, err
}

func NewGlobalNodeInfoTable() *GlobalNodeInfoTable {
	return &GlobalNodeInfoTable{
		nodeIDs: make([]UniqueID, 0),
		infos:   make(map[UniqueID]*NodeInfo),
		clients: make(map[UniqueID]NodeClient),
	}
}