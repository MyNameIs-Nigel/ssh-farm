package leaderboard

import (
	"context"
	"testing"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func TestContractMetadataAndConditionalSuffixes(t *testing.T) {
	clock := newFakeClock()
	now := clock.now().Unix()
	src := &fakeSource{rows: []store.LeaderboardRow{
		{Fingerprint: `SHA256:one`, Slot: `farm`, Coins: 400, FarmName: `Same Farm`, UpdatedAt: now, ContractsCompleted: 1, LeaderboardNameStyle: `leaf`},
		{Fingerprint: `SHA256:two`, Slot: `farm`, Coins: 300, FarmName: `same farm`, UpdatedAt: now, ContractsCompleted: 2, LeaderboardNameStyle: `gold`},
		{Fingerprint: `SHA256:three`, Slot: `farm`, Coins: 200, FarmName: `Unique`, UpdatedAt: now, ContractsCompleted: 3, LeaderboardNameStyle: `purple_wave`},
		{Fingerprint: `SHA256:four`, Slot: `farm`, Coins: 100, FarmName: ``, UpdatedAt: now},
	}}
	board, err := New(src, time.Minute, 0, 1, clock.now).Get(context.Background(), ref(`SHA256:one`))
	if err != nil {
		t.Fatal(err)
	}
	if !board.Top[0].ShowSuffix || !board.Top[1].ShowSuffix {
		t.Fatal(`both duplicate display names must show fingerprint suffixes`)
	}
	if board.Top[2].ShowSuffix {
		t.Fatal(`a unique named farm must omit its fingerprint suffix`)
	}
	if !board.Top[3].ShowSuffix {
		t.Fatal(`the generic unnamed FARM fallback must always show its suffix`)
	}
	if board.Top[2].ContractsCompleted != 3 || board.Top[2].NameStyle != `purple_wave` {
		t.Fatalf(`contract metadata = %+v`, board.Top[2])
	}
}

func TestDuplicateCheckUsesPostModerationDisplayNamesAcrossWholePopulation(t *testing.T) {
	clock := newFakeClock()
	now := clock.now().Unix()
	rows := []store.LeaderboardRow{
		{Fingerprint: `SHA256:a`, Slot: `farm`, Coins: 100, FarmName: `Duplicate`, UpdatedAt: now},
		{Fingerprint: `SHA256:b`, Slot: `farm`, Coins: 90, FarmName: `DUPLICATE`, UpdatedAt: now},
	}
	board, err := New(&fakeSource{rows: rows}, time.Minute, 0, 1, clock.now).Get(context.Background(), SaveRef{})
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range board.Top {
		if !row.ShowSuffix {
			t.Fatalf(`Top[%d].ShowSuffix = false, want true`, i)
		}
	}
}
