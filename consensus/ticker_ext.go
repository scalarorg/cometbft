package consensus

import (
	"github.com/cometbft/cometbft/libs/log"
)

// mock ticker only fires on RoundStepNewHeight
// and only once if onlyOnce=true
type MockTicker struct {
}

func (m *MockTicker) Start() error {
	return nil
}

func (m *MockTicker) Stop() error {
	return nil
}

func (m *MockTicker) ScheduleTimeout(ti timeoutInfo) {

}

func (m *MockTicker) Chan() <-chan timeoutInfo {
	c := make(chan timeoutInfo, 1)
	return c
}

func (*MockTicker) SetLogger(log.Logger) {}

func NewMockTimeoutTicker() TimeoutTicker {
	tt := &MockTicker{}
	return tt
}
