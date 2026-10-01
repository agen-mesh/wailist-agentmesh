package nodes

import "strings"

// The bounds on one replayed turn, in characters.
//
// A model turn gets the smaller one: it is a summary of a build the graph
// itself already records and resends on every call, so its tail is the
// cheapest thing in the request to lose.
//
// A user turn gets a much larger one, because it is what a follow-up ("use
// the specs I gave you") refers back to. It is bounded at all because "a user
// turn is short" holds right up until someone pastes a log or a spec into the
// chat, and then that paste is replayed on every round of every later build
// for as long as it stays in the window -- a 4,000-token paste costs roughly
// a cent a build, on six builds, for text the model has already acted on.
//
// Only turns replayed as HISTORY are clipped. The message being answered now
// is appended separately, whole, alongside the graph snapshot (buildPayload),
// so nothing here can truncate the request the user just made.
const (
	maxReplayedModelChars = 600
	maxReplayedUserChars  = 2000
)

// The notes withTestStatus appends to a builder reply. Declared once and used
// by both sides, so rewording one cannot silently stop clipHistoryText from
// finding it -- which would put stale test values straight back into the
// replayed history, the thing it exists to prevent.
const (
	trailerTestedAnswer = "\n\n**Test run answer:** "
	trailerUntested     = "\n\n_This workflow has not been test-run since it was last changed, so it has not been checked yet._"
	trailerTestNoAnswer = "\n\n_The last test run did not produce an answer: "
	trailerNotChecked   = "\n\n_Not checked by the test run: "
	// A test run that failed a step AND still answered -- a degraded read
	// does both. Distinct from trailerTestNoAnswer, which claims there was
	// no answer at all.
	trailerTestPartial = "\n\n_A step failed during the test run, so this answer is partial: "
)

var testRunTrailers = []string{
	trailerTestedAnswer, trailerUntested, trailerTestNoAnswer,
	trailerNotChecked, trailerTestPartial,
}

// clipHistoryText trims one prior turn before it is replayed to the model.
//
// A model turn loses its test-run notes entirely, then is clipped. Those
// notes restate a test result for a graph that has since changed: replaying
// them costs tokens on every round of every later build, and a value quoted
// in one ("BTC is $64,102.11") is exactly the kind of number a model then
// repeats to the user as if it were current.
//
// A user turn keeps everything it says and only loses a long tail.
func clipHistoryText(role, text string) string {
	if role != "model" {
		return clip(text, maxReplayedUserChars)
	}
	for _, trailer := range testRunTrailers {
		if i := strings.Index(text, trailer); i >= 0 {
			text = text[:i]
		}
	}
	return clip(strings.TrimRight(text, " \n"), maxReplayedModelChars)
}
