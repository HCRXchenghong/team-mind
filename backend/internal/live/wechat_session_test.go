package live

import (
	"sync"
	"testing"
)

func TestWechatSessionStateConcurrentUpdatesKeepIndependentFields(t *testing.T) {
	for round := 0; round < 100; round++ {
		session := &wechatSession{}
		session.state.Store(&wechatSessionState{Status: "wait"})
		updates := []func(*wechatSessionState){
			func(state *wechatSessionState) { state.Status = "scaned" },
			func(state *wechatSessionState) { state.VerifyCode = "4821" },
			func(state *wechatSessionState) { state.Error = "test error" },
			func(state *wechatSessionState) { state.Done = true },
		}
		start := make(chan struct{})
		var wait sync.WaitGroup
		for _, update := range updates {
			update := update
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				session.updateState(update)
			}()
		}
		close(start)
		wait.Wait()

		state := session.snapshot()
		if state.Status != "scaned" || state.VerifyCode != "4821" || state.Error != "test error" || !state.Done {
			t.Fatalf("concurrent snapshot updates lost state: %+v", state)
		}
	}
}
