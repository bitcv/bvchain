package obj

import (
)


type Serializable interface {
	Serialize() []byte
	Unserialze(buf []byte)
}

