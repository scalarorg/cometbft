package service

import (
	"sync/atomic"
)

// Sleep implements Service by just setting the stopped flag.
func (bs *BaseService) Sleep() {
	atomic.SwapUint32(&bs.stopped, 1)
	atomic.SwapUint32(&bs.started, 0)
}

// Wake implements Service by just setting the started flag.
func (bs *BaseService) Wake() {
	atomic.SwapUint32(&bs.started, 1)
	atomic.SwapUint32(&bs.stopped, 0)
}
