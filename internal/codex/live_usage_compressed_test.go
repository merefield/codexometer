package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compressTestRollout(t *testing.T, path string, remove bool) []byte {
	t.Helper()
	plain, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := encoder.EncodeAll(plain, nil)
	encoder.Close()
	if err := os.WriteFile(path+".zst", data, 0600); err != nil {
		t.Fatal(err)
	}
	if remove {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	return plain
}

func TestCompressedHistoryRestorationCountsOnlyFreshResponses(t *testing.T) {
	_, path, now := astraUsageReader(t, 100)
	usage := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	old := exactLine(t, now, "astra-accounting", "turn", "historical", usage) + "\n"
	appendRollout(t, path, old)
	plain := compressTestRollout(t, path, true)
	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	baseline, err := r.FetchTokenUsage(context.Background())
	if err != nil || baseline.TotalTokens != 0 || len(r.files) != 1 || !r.files[path].compressed || !r.files[path].exactUsage {
		t.Fatal("compressed history was lost or billed", baseline, err)
	}
	// Codex restores history and can append before our next discovery pass.
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, old+exactLine(t, now, "astra-accounting", "turn", "new", usage)+"\n")
	fresh, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || fresh.TotalTokens != 1100 || fresh.APIEqPricedCalls != 1 || len(r.files) != 1 || r.files[path].compressed {
		t.Fatal("restoration billed history or missed append", fresh, err)
	}
	next, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || next.TotalTokens != fresh.TotalTokens || next.APIEqUSD != fresh.APIEqUSD {
		t.Fatal("plain/compressed siblings duplicated accounting", next, err)
	}
}

func TestCompressionWhileObservedPreservesSessionTotals(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	usage := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "turn", "first", usage)+"\n")
	before, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plain := compressTestRollout(t, path, true)
	compressed, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || compressed.TotalTokens != before.TotalTokens || compressed.APIEqUSD != before.APIEqUSD || len(r.files) != 1 || !r.files[path].compressed {
		t.Fatal("compression discarded or recounted observed history", compressed, err)
	}
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "turn", "second", usage)+"\n")
	restored, err := r.FetchTokenUsage(context.Background())
	if err != nil || restored.TotalTokens != 2200 || restored.APIEqPricedCalls != 2 || restored.Sessions[0].TotalTokens != 2200 {
		t.Fatal("restoration lost existing totals", restored, err)
	}
}

func TestChangedCompressedRestorationRebasesCopiedHistory(t *testing.T) {
	_, path, now := astraUsageReader(t, 100)
	usage := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "turn", "old", usage)+"\n")
	plain := compressTestRollout(t, path, true)
	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Revert/rewrite changes the baseline prefix; none of this is fresh spend.
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(plain), "old", "rewritten")), 0600); err != nil {
		t.Fatal(err)
	}
	rebased, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || rebased.TotalTokens != 0 || rebased.APIEqPricedCalls != 0 || rebased.APIEqAccountingAdjustments != 1 {
		t.Fatal("changed restoration billed copied history", rebased, err)
	}
}

func TestCorruptCompressedHistoryIsRejected(t *testing.T) {
	_, path, _ := astraUsageReader(t, 100)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".zst", []byte("invalid zstd"), 0600); err != nil {
		t.Fatal(err)
	}
	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	if _, err := r.FetchTokenUsage(context.Background()); err == nil {
		t.Fatal("corrupt compressed history accepted")
	}
	if len(r.files) != 0 {
		t.Fatal("partial compressed baseline retained")
	}
}

func TestCompressedRestorationChecksContextBeforeUnchangedTail(t *testing.T) {
	_, path, now := astraUsageReader(t, 100)
	// Put changed model context outside the suffix check, retaining byte length.
	appendRollout(t, path, strings.Repeat(`{"type":"response_item","payload":"padding"}`+"\n", 200))
	usage := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "turn", "old", usage)+"\n")
	plain := compressTestRollout(t, path, true)
	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(string(plain), "gpt-6-astra", "gpt-6.1-sol")
	if len(changed) != len(plain) || changed == string(plain) {
		t.Fatal("test did not preserve history size")
	}
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || got.APIEqAccountingAdjustments != 1 || got.APIEqPricedCalls != 0 || r.files[path].currentModel != "gpt-6.1-sol" {
		t.Fatal("changed context trusted because suffix matched", got, err)
	}
}
