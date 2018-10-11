package node

import (
	"bvchain/util"
	"bvchain/util/log"
	"bvchain/cfg"
	"bvchain/p2p"
	"bvchain/p2p/pex"
	"bvchain/trie"
	"bvchain/version"
	"bvchain/state"
	"bvchain/db"
	"bvchain/genesis"
	"bvchain/ec"
	"bvchain/chain"

	"fmt"
	"path/filepath"
)

// Node is the highest level interface to a full node.
// It includes all configuration information and running services.
type Node struct {
	util.BaseService

	// config
	config *cfg.Config
	genesisDoc *genesis.Document

	// network
	adapter *p2p.Adapter  // p2p connections
	addrBook pex.AddrBook // known peers

	stateDb *trie.Trie
	blockDb db.KvDb
}

// NewNode returns a new, ready to go, Node.
func NewNode(config *cfg.Config, logger log.Logger) (*Node, error) {

	blockDbPath := filepath.Join(config.DbPath, "block")
	blockDb, err := db.NewLevelDb(blockDbPath, 0, 0); // TODO
	if err != nil {
		return nil, err
	}

	stateDbPath := filepath.Join(config.DbPath, "state")
	kvdb, err := db.NewLevelDb(stateDbPath, 0, 0);
	stateDb, err := trie.NewWithCacheDb(ec.Hash256{}, trie.NewCacheDb(kvdb)) // TODO
	if err != nil {
		return nil, err
	}

	genDoc, err := genesis.DocumentFromDb(stateDb)
	if err != nil {
		genDoc, err = genesis.DocumentFromFile(config.GenesisFile)
		if err != nil {
			return nil, err
		}
		genDoc.SaveToDb(stateDb)
	}

	chainState, err := state.LoadChainStateFromDbOrGenesisDoc(stateDb, genDoc)
	if err != nil {
		return nil, err
	}

	// Create the proxyApp, which manages connections (consensus, mempool, query)
	// and sync and the app by performing a handshake
	// and replaying any necessary blocks

	consensusLogger := logger.With("module", "consensus")
/* 	TODO
	handshaker := consensus.NewHandshaker(stateDb, chainState, blockDb, genDoc)
	handshaker.SetLogger(consensusLogger)
*/

	// reload the chainState (it may have been updated by the handshake)
	chainState = state.LoadChainState(stateDb)

	// TODO
	selfWitness := chain.Witness{}

	// Decide whether to fast-sync or not
	// We don't fast-sync when the only validator is us.
	/*
	fastSync := config.FastSync
	if chainState.Wset.Size() == 1 {
		w := chainState.Wset.GetByIndex(0)
		if w.Address.Equal(selfWitness.Address) {
			fastSync = false
		}
	}
	*/

	// Log whether this node is a validator or an observer
	if chainState.Wset.HasAddress(selfWitness.Address) {
		consensusLogger.Info("This node is a witness", "addr", selfWitness.Address)
	} else {
		consensusLogger.Info("This node is not a witness", "addr", selfWitness.Address)
	}

	p2pLogger := logger.With("module", "p2p")

	adapter := p2p.NewAdapter(config.P2p)
	adapter.SetLogger(p2pLogger)

	// Optionally, start the pex reactor
	//
	// TODO:
	//
	// We need to set Seeds and PersistentPeers on the adapter,
	// since it needs to be able to use these (and their DNS names)
	// even if the Pex is off. We can include the DNS name in the NetAddress,
	// but it would still be nice to have a clear list of the current "PersistentPeers"
	// somewhere that we can return with net_info.
	//
	// If Pex is on, it should handle dialing the seeds. Otherwise the adapter does it.
	// Note we currently use the addrBook regardless at least for AddOurAddress
	addrBook := pex.NewAddrBook(config.P2p.AddrBook, config.P2p.AddrBookStrict)
	addrBook.SetLogger(p2pLogger.With("book", config.P2p.AddrBook))
	if config.P2p.PexReactor {
		// TODO persistent peers ? so we can have their DNS addrs saved
		pexReactor := pex.NewPexReactor(addrBook,
			&pex.PexReactorConfig{
				Seeds:          config.P2p.Seeds,
				SeedMode:       config.P2p.SeedMode,
				PrivatePeerIds: config.P2p.PrivatePeerIds,
			})
		pexReactor.SetLogger(p2pLogger)
		adapter.AddReactor("Pex", pexReactor)
	}

	adapter.SetAddrBook(addrBook)

	node := &Node{
		config: config,
		genesisDoc: genDoc,

		adapter: adapter,
		addrBook: addrBook,

		stateDb: stateDb,
		blockDb: blockDb,
	}
	node.BaseService.Init(logger, "Node", node)
	return node, nil
}

// OnStart starts the Node. It implements util.Service.
func (n *Node) OnStart() error {
	// Create & add listener
	l := p2p.NewListener(
		n.config.P2p.ListenAddr,
		n.config.P2p.ExternalAddr,
		n.config.P2p.Upnp,
		n.Logger.With("module", "p2p"))
	n.adapter.AddListener(l)

	// Generate node PrivKey
	nodeKey, err := p2p.LoadOrGenNodeKey(n.config.NodeKeyFile)
	if err != nil {
		return err
	}
	n.Logger.Info("P2p NodeId", "NodeId", nodeKey.NodeId(), "file", n.config.NodeKeyFile)

	nodeInfo := n.makeNodeInfo(nodeKey.NodeId())
	n.adapter.SetNodeInfo(nodeInfo)
	n.adapter.SetNodeKey(nodeKey)

	// Add ourselves to addrbook to prevent dialing ourselves
	n.addrBook.AddOurAddress(nodeInfo.NetAddress())

	// Start the RPC server before the P2P server
	// so we can eg. receive txs for the first block
	if len(n.config.Rpc.ListenAddrs) > 0 {
		// TODO
	}

	// Start the adapter (the P2P server).
	err = n.adapter.Start()
	if err != nil {
		return err
	}

	// Always connect to persistent peers
	if len(n.config.P2p.PersistentPeers) > 0 {
		err = n.adapter.DialPeersAsync(n.addrBook, n.config.P2p.PersistentPeers, true)
		if err != nil {
			return err
		}
	}

	return nil
}

// OnStop stops the Node. It implements util.Service.
func (n *Node) OnStop() {
	n.BaseService.OnStop()

	n.Logger.Info("Stopping Node")
	n.adapter.Stop()

	// TODO: stop rpc server
}

// AddListener adds a listener to accept inbound peer connections.
// It should be called before starting the Node.
// The first listener is the primary listener (in NodeInfo)
func (n *Node) AddListener(l p2p.Listener) {
	n.adapter.AddListener(l)
}

// ConfigureRpc sets all variables in rpccore so they will serve
// rpc calls from this node
func (n *Node) ConfigureRpc() {
	// TODO
}

func (n *Node) startRpc() error {
	n.ConfigureRpc()

	// TODO

	return nil
}

// Adapter returns the Node's Adapter.
func (n *Node) Adapter() *p2p.Adapter {
	return n.adapter
}

// GenesisDoc returns the Node's GenesisDoc.
func (n *Node) GenesisDoc() *genesis.Document {
	return n.genesisDoc
}

func (n *Node) makeNodeInfo(nodeId p2p.NodeId) *p2p.NodeInfo {
	nodeInfo := &p2p.NodeInfo{
		NodeId:  nodeId,
		ChainId: n.genesisDoc.ChainId,
		Version: version.Version,
	}

	if !n.adapter.IsListening() {
		return nodeInfo
	}

	p2pListener := n.adapter.Listeners()[0]
	p2pHost := p2pListener.ExternalAddressHost()
	p2pPort := p2pListener.ExternalAddress().Port
	nodeInfo.ListenAddr = fmt.Sprintf("%v:%v", p2pHost, p2pPort)

	return nodeInfo
}

//------------------------------------------------------------------------------

// NodeInfo returns the Node's Info from the Adapter.
func (n *Node) NodeInfo() *p2p.NodeInfo {
	return n.adapter.NodeInfo()
}


