package gitadapter_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
)

func TestAdapter_ReadsDuringFetchDoNotFail(t *testing.T) {
	upDir, up := newUpstream(t)
	commitFiles(t, up, upDir, map[string]string{"dev/go/agents/a.md": "a0"}, "c0")

	clone := filepath.Join(t.TempDir(), "clone")
	a := gitadapter.New()
	require.NoError(t, a.Clone("file://"+upDir, clone))

	var done atomic.Bool
	var wg sync.WaitGroup
	var mu sync.Mutex
	var readErrs []error

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer done.Store(true)
		for i := range 12 {
			commitFiles(t, up, upDir, map[string]string{fmt.Sprintf("dev/go/agents/f%d.md", i): "x"}, "c")
			if _, err := a.Fetch(clone, "master"); err != nil {
				mu.Lock()
				readErrs = append(readErrs, fmt.Errorf("fetch: %v", err))
				mu.Unlock()
			}
		}
	}()

	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !done.Load() {
				if _, err := a.RemoteHEAD(clone, "master"); err != nil {
					mu.Lock()
					readErrs = append(readErrs, fmt.Errorf("remote head: %v", err))
					mu.Unlock()
				}
				if _, err := a.ReadMarketFiles(clone, "master"); err != nil {
					mu.Lock()
					readErrs = append(readErrs, fmt.Errorf("read market files: %v", err))
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	require.Empty(t, readErrs)
}
