package p2p

import (
	"bvchain/ec"
	"bvchain/util"

	"fmt"
	"io/ioutil"
	"encoding/json"
)

type NodeId string


//------------------------------------------------------------------------------
// Persistent node id

type NodeKey struct {
	PrivKey ec.PrivKey
}


func IsValidNodeId(id string) bool {
	bz, err := util.Base32SumToBytes(id, "", 0, false)
	if err != nil {
		return false
	}
	if len(bz) != 32 {
		return false
	}
	return true
}

// NodeId returns the node id
func (key *NodeKey) NodeId() NodeId {
	return PubKeyToNodeId(key.PubKey())
}

// PubKey returns the node's PubKey
func (key *NodeKey) PubKey() ec.PubKey {
	return key.PrivKey.PubKey()
}

func PubKeyToNodeId(p ec.PubKey) NodeId {
	hash := ec.Sum256(p[:])
	return NodeId(util.BytesToBase32Sum(hash[:], "", 0, false))
}

// LoadOrGenNodeKey attempts to load the NodeKey from the given filePath.
// If the file does not exist, it generates and saves a new NodeKey.
func LoadOrGenNodeKey(filePath string) (*NodeKey, error) {
	if util.FileExists(filePath) {
		key, err := LoadNodeKey(filePath)
		if err != nil {
			return nil, err
		}
		return key, nil
	}
	return generateNodeKey(filePath)
}

func LoadNodeKey(filePath string) (*NodeKey, error) {
	jsonBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	key := new(NodeKey)
	err = json.Unmarshal(jsonBytes, key)

	if err != nil {
		return nil, fmt.Errorf("Error reading NodeKey from %v: %v", filePath, err)
	}
	return key, nil
}

func generateNodeKey(filePath string) (*NodeKey, error) {
	key := &NodeKey{ PrivKey: ec.NewPrivKey() }

	jsonBytes, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return nil, err
	}

	err = ioutil.WriteFile(filePath, jsonBytes, 0600)
	if err != nil {
		return nil, err
	}
	return key, nil
}

