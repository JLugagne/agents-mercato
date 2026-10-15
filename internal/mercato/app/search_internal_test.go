package app

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenize_ConcurrentUse(t *testing.T) {
	const text = "running agents orchestrating deployments generalization nationalities"
	want := tokenize(text)

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 2000 {
				require.Equal(t, want, tokenize(text))
			}
		})
	}
	wg.Wait()
}
