package email

import (
	"errors"
	"testing"
	"time"
)

// fakeFetcher answers getFullItemsBatch from a programmable script.
// errOnBatch maps "first ID of batch" → error to return.
// failSingles is a set of raw IDs that always error when fetched solo.
type fakeFetcher struct {
	calls       int
	errOnBatch  map[string]error // returned only the first time the batch is seen
	failSingles map[string]bool
	seenBatches map[string]int // first-ID → number of attempts for that batch
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		errOnBatch:  map[string]error{},
		failSingles: map[string]bool{},
		seenBatches: map[string]int{},
	}
}

func (f *fakeFetcher) getFullItemsBatch(rawIDs []string) ([]ewsFullItem, error) {
	f.calls++
	if len(rawIDs) == 0 {
		return nil, nil
	}

	// Single-ID call (split path) → fail if listed.
	if len(rawIDs) == 1 {
		if f.failSingles[rawIDs[0]] {
			return nil, errors.New("single fetch failed: " + rawIDs[0])
		}
		return []ewsFullItem{makeFakeItem(rawIDs[0])}, nil
	}

	key := rawIDs[0]
	f.seenBatches[key]++
	if err, ok := f.errOnBatch[key]; ok && f.seenBatches[key] == 1 {
		return nil, err
	}
	// On second attempt or if no scripted error: succeed with all items.
	out := make([]ewsFullItem, 0, len(rawIDs))
	for _, id := range rawIDs {
		out = append(out, makeFakeItem(id))
	}
	return out, nil
}

func makeFakeItem(rawID string) ewsFullItem {
	return ewsFullItem{
		ewsItem: ewsItem{
			ItemID:  ewsItemID{ID: rawID},
			Subject: "S-" + rawID,
		},
		Body: ewsBodyContent{BodyType: "Text", Content: "body-" + rawID},
	}
}

// makeIDs builds n IDs prefixed so each batch (size 20) has a distinct first ID.
func makeIDs(n int) []string {
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = idAt(i)
	}
	return ids
}

func idAt(i int) string {
	return "ID" + padNum(i)
}

func padNum(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestFetchEmailsByIDs_BatchErrorDoesNotAbortRun(t *testing.T) {
	// Speed up tests: zero out the sleeps.
	oldSleep, oldRetry := batchSleep, batchRetryDelay
	batchSleep = 0
	batchRetryDelay = 0
	defer func() { batchSleep = oldSleep; batchRetryDelay = oldRetry }()

	ids := makeIDs(80) // 4 batches of 20

	fake := newFakeFetcher()
	// Batch 2 (ids[20]) errors on first attempt — retry should succeed.
	fake.errOnBatch[idAt(20)] = errors.New("transient timeout")

	res := fetchEmailsByIDs(fake, ids, &BatchFetchResult{Succeeded: map[string]*EmailDetail{}})

	if len(res.Succeeded) != 80 {
		t.Fatalf("expected 80 succeeded after retry, got %d", len(res.Succeeded))
	}
	if len(res.FailedIDs) != 0 {
		t.Fatalf("expected 0 failed, got %d", len(res.FailedIDs))
	}
	// Verify items from batches 1, 3, 4 are present (not just batch 2's retry).
	for _, i := range []int{0, 19, 40, 79} {
		raw := idAt(i)
		if _, ok := res.Succeeded[EncodeItemID(raw)]; !ok {
			t.Errorf("missing %s — non-failing batches must still land in result", raw)
		}
	}
}

func TestFetchEmailsByIDs_SplitFallback_PartialFailure(t *testing.T) {
	oldSleep, oldRetry := batchSleep, batchRetryDelay
	batchSleep = 0
	batchRetryDelay = 0
	defer func() { batchSleep = oldSleep; batchRetryDelay = oldRetry }()

	ids := makeIDs(20) // 1 batch of 20

	fake := newFakeFetcher()
	// Make the batch fail twice (first call + retry) → forces split path.
	fake.errOnBatch[idAt(0)] = errors.New("hard failure")
	// Override: fail on every attempt while batch size > 1.
	originalFn := fake.getFullItemsBatch
	_ = originalFn
	// Replace with a hand-rolled fetcher: any batch >1 errors, singles use failSingles.
	fake2 := &scriptedFetcher{failBatch: true, failSingles: map[string]bool{
		idAt(3):  true,
		idAt(17): true,
	}}

	res := fetchEmailsByIDs(fake2, ids, &BatchFetchResult{Succeeded: map[string]*EmailDetail{}})

	if got := len(res.Succeeded); got != 18 {
		t.Fatalf("expected 18 succeeded, got %d", got)
	}
	if got := len(res.FailedIDs); got != 2 {
		t.Fatalf("expected 2 failed, got %d", got)
	}
	// FailedIDs must be exactly the ones we marked.
	failed := map[string]bool{}
	for _, id := range res.FailedIDs {
		failed[id] = true
	}
	if !failed[idAt(3)] || !failed[idAt(17)] {
		t.Fatalf("expected failures for ID003 and ID017, got %v", res.FailedIDs)
	}
	if len(res.Errors) < 2 {
		t.Fatalf("expected >= 2 error entries, got %d", len(res.Errors))
	}
}

// scriptedFetcher always fails multi-ID batches and only succeeds for singles
// not listed in failSingles.
type scriptedFetcher struct {
	failBatch   bool
	failSingles map[string]bool
}

func (s *scriptedFetcher) getFullItemsBatch(rawIDs []string) ([]ewsFullItem, error) {
	if len(rawIDs) > 1 && s.failBatch {
		return nil, errors.New("batch refused")
	}
	if len(rawIDs) == 1 && s.failSingles[rawIDs[0]] {
		return nil, errors.New("single failed: " + rawIDs[0])
	}
	out := make([]ewsFullItem, 0, len(rawIDs))
	for _, id := range rawIDs {
		out = append(out, makeFakeItem(id))
	}
	return out, nil
}

func TestIsThrottleError(t *testing.T) {
	cases := map[string]bool{
		"EWS returned HTTP 503":        true,
		"<faultcode>ErrorServerBusy<": true,
		"ServerBusy: back off":         true,
		"some random error":            false,
		"":                             false,
	}
	for msg, want := range cases {
		var err error
		if msg != "" {
			err = errors.New(msg)
		}
		if got := isThrottleError(err); got != want {
			t.Errorf("isThrottleError(%q) = %v, want %v", msg, got, want)
		}
	}
}

// Sanity check that batchSleep is honoured between batches — guards against
// accidental removal of throttle protection.
func TestFetchEmailsByIDs_HonoursBatchSleep(t *testing.T) {
	oldSleep, oldRetry := batchSleep, batchRetryDelay
	batchSleep = 30 * time.Millisecond
	batchRetryDelay = 0
	defer func() { batchSleep = oldSleep; batchRetryDelay = oldRetry }()

	ids := makeIDs(40) // 2 batches → 1 sleep
	fake := newFakeFetcher()

	start := time.Now()
	fetchEmailsByIDs(fake, ids, &BatchFetchResult{Succeeded: map[string]*EmailDetail{}})
	elapsed := time.Since(start)

	if elapsed < 25*time.Millisecond {
		t.Fatalf("expected >= ~30ms sleep between batches, ran in %v", elapsed)
	}
}
