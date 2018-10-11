package cfg

import (
	"testing"
	"fmt"
	"bytes"
	"github.com/BurntSushi/toml"
)

func TestConfig(t *testing.T) {
	config := defaultConfig()
	b := &bytes.Buffer{}
	enc := toml.NewEncoder(b)
	enc.Indent = ""
	enc.Encode(config)
	fmt.Println(string(b.Bytes()))
}

