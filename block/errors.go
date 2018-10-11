package block

import (
	"fmt"
)

type ErrInvalidBlock error
type ErrProxyAppConn error

type ErrUnknownBlock struct {
	Height int64
}

func (e ErrUnknownBlock) Error() string {
	return fmt.Sprintf("Could not find block #%d", e.Height)
}


