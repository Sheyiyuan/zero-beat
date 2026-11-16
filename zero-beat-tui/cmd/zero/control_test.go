package main

import "testing"

func TestPlainFrameStripsStyleAndTrailingSpace(t *testing.T) {
	frame := "\x1b[31m曲库\x1b[0m   \n第二行\t  \n"
	if got, want := plainFrame(frame), "曲库\n第二行"; got != want {
		t.Fatalf("plainFrame = %q，应为 %q", got, want)
	}
}
