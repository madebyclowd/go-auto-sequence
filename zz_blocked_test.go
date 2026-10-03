package sequence

import "testing"

// Throwaway: proves a PR with a failing Tests check cannot be merged. Never merged.
func TestIntentionalFailure(t *testing.T) { t.Fatal("intentional failure for branch-protection check") }
