package api

import (
	"context"
	"testing"

	"lpicto/backend/internal/model"
)

func TestBoundedByteRange(t *testing.T) {
	const size int64 = 100 << 20
	tests := []struct {
		name       string
		header     string
		wantStart  int64
		wantEnd    int64
		shouldFail bool
	}{
		{name: "open ended reaches file end", header: "bytes=0-", wantStart: 0, wantEnd: size - 1},
		{name: "explicit range is preserved", header: "bytes=1048576-99999999", wantStart: 1 << 20, wantEnd: 99999999},
		{name: "small explicit range is preserved", header: "bytes=100-199", wantStart: 100, wantEnd: 199},
		{name: "suffix is preserved", header: "bytes=-33554432", wantStart: 68 << 20, wantEnd: size - 1},
		{name: "range at end", header: "bytes=104857590-", wantStart: 104857590, wantEnd: size - 1},
		{name: "multiple ranges rejected", header: "bytes=0-1,10-11", shouldFail: true},
		{name: "past end rejected", header: "bytes=104857600-", shouldFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, err := boundedByteRange(test.header, size, 0)
			if test.shouldFail {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != test.wantStart || end != test.wantEnd {
				t.Fatalf("got %d-%d, want %d-%d", start, end, test.wantStart, test.wantEnd)
			}
		})
	}
}

func TestExpectedVideoChunkBytes(t *testing.T) {
	const size int64 = 20<<20 + 123
	tests := []struct {
		name  string
		index int64
		want  int64
	}{
		{name: "first full chunk", index: 0, want: directVideoChunkBytes},
		{name: "second full chunk", index: 1, want: directVideoChunkBytes},
		{name: "final partial chunk", index: 2, want: 4<<20 + 123},
		{name: "past end", index: 3, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := expectedVideoChunkBytes(size, test.index); got != test.want {
				t.Fatalf("got %d bytes, want %d", got, test.want)
			}
		})
	}
}

func TestDirectVideoChunkRangeFullWarmsRemainder(t *testing.T) {
	duration := 100.0
	asset := model.Asset{Size: 20 << 20, Duration: &duration}
	first, last := directVideoChunkRange(asset, 50, true)
	if first != 1 || last != 2 {
		t.Fatalf("full range = %d-%d, want 1-2", first, last)
	}
	first, last = directVideoChunkRange(asset, 50, false)
	if first != 1 || last != 1 {
		t.Fatalf("short range = %d-%d, want 1-1", first, last)
	}
}

func TestVideoFullWarmJobDeduplicatesAndCancelsByAsset(t *testing.T) {
	server := &Server{videoFullWarmJobs: map[string]*videoFullWarmJob{}}
	ctx, cancel := context.WithCancel(context.Background())
	job := &videoFullWarmJob{cancel: cancel}
	if !server.registerVideoFullWarmJob("direct:asset", job) {
		t.Fatal("first job should register")
	}
	if server.registerVideoFullWarmJob("direct:asset", &videoFullWarmJob{cancel: func() {}}) {
		t.Fatal("duplicate job should be rejected")
	}
	server.cancelVideoFullWarm("asset")
	if ctx.Err() == nil {
		t.Fatal("asset warm job was not cancelled")
	}
	if len(server.videoFullWarmJobs) != 0 {
		t.Fatalf("jobs remain after cancellation: %d", len(server.videoFullWarmJobs))
	}
}
