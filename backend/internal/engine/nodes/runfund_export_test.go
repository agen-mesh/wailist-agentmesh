package nodes

import "time"

// Test-only accessors for runfund.go's unexported retry schedule, so
// runfund_test.go (package nodes_test) derives its real-elapsed-time
// bounds from the same values the production code uses instead of
// hand-mirrored copies that silently drift when the constants change.

// SelfSettleMaxAttemptsForTest is selfSettleMaxAttempts.
const SelfSettleMaxAttemptsForTest = selfSettleMaxAttempts

// WorstCaseBackoffTotalForTest is the most time a full retry sequence can
// spend sleeping between attempts, per the real per-attempt backoff
// schedule (not a flat max-per-gap approximation) -- see
// worstCaseBackoffTotal.
func WorstCaseBackoffTotalForTest() time.Duration { return worstCaseBackoffTotal() }
