package genesis

import (
	"bvchain/util"
	"bvchain/ec"
	"bvchain/chain"
	"bvchain/trie"

	"encoding/json"
	"io/ioutil"
	"time"
	"fmt"
	"errors"
	"halftwo/mangos/vbs"
	"halftwo/mangos/xerr"
)

// GenesisWitness is an initial witness.
type GenesisWitness struct {
	PubKey ec.PubKey
}

// Document defines the initial conditions for a blockchain, in particular its witness set.
type Document struct {
	ChainId     string
	GenesisTime int64
	Witnesses   []GenesisWitness
	ChainParams *chain.ChainParams
}

// SaveAs is a utility method for saving GenensisDoc as a JSON file.
func (doc *Document) SaveAs(file string) error {
	bz, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFile(file, bz, 0644)
}

// ValidateAndComplete checks that all necessary fields are present
// and fills in defaults for optional fields left empty
func (doc *Document) ValidateAndComplete() error {
	if doc.ChainId == "" {
		return fmt.Errorf("Genesis doc must include non-empty ChainId")
	}

	if doc.ChainParams == nil {
		doc.ChainParams = chain.DefaultChainParams()
	} else {
		if err := doc.ChainParams.Validate(); err != nil {
			return err
		}
	}

	if len(doc.Witnesses) == 0 {
		return fmt.Errorf("The genesis file must have at least one witness")
	}

	for _, w := range doc.Witnesses {
                if w.PubKey.IsZero() {
                        return fmt.Errorf("The genesis file cannot contain witness with zero pubkey: %v", w)
                }
        }

	if doc.GenesisTime == 0 {
		doc.GenesisTime = time.Now().Unix()
	}

	return nil
}

//------------------------------------------------------------
// Make genesis state from file

// DocumentFromJson unmarshalls JSON data into a Document.
func DocumentFromJson(jsonBlob []byte) (*Document, error) {
	doc := Document{}
	err := json.Unmarshal(jsonBlob, &doc)
	if err != nil {
		return nil, err
	}

	if err := doc.ValidateAndComplete(); err != nil {
		return nil, err
	}

	return &doc, err
}

// DocumentFromFile reads JSON data from a file and unmarshalls it into a Document.
func DocumentFromFile(docFile string) (*Document, error) {
	jsonBlob, err := ioutil.ReadFile(docFile)
	if err != nil {
		return nil, xerr.Trace(err, "Couldn't read genesis Document file")
	}
	doc, err := DocumentFromJson(jsonBlob)
	if err != nil {
		return nil, xerr.Tracef(err, "Error reading genesis Document at %v", docFile)
	}
	return doc, nil
}

var genesisDocKey = []byte("genesisDoc")

func DocumentFromDb(db *trie.Trie) (*Document, error) {
	bz := db.Get(genesisDocKey)
	if len(bz) == 0 {
		return nil, errors.New("Genesis doc not found")
	}
	var doc Document
	err := vbs.Unmarshal(bz, doc)
	util.AssertNoError(err)
	return &doc, nil
}

func (doc *Document) SaveToDb(db *trie.Trie) {
	bz, err := vbs.Marshal(doc)
	util.AssertNoError(err)
	db.Update(genesisDocKey, bz)
}

