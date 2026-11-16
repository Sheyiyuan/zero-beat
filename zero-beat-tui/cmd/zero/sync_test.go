package main

import "testing"

func TestParseResolveArgs(t *testing.T) {
	cases := []struct {
		args    []string
		want    resolveArgs
		wantErr bool
	}{
		{[]string{"tx1", "playlist:local-1", "--keep", "local"},
			resolveArgs{txid: "tx1", entity: "playlist:local-1", keep: "local"}, false},
		{[]string{"tx1", "--all", "remote"},
			resolveArgs{txid: "tx1", all: "remote"}, false},
		{[]string{"--all=both", "tx1"},
			resolveArgs{txid: "tx1", all: "both"}, false},
		{[]string{"tx1", "--discard"},
			resolveArgs{txid: "tx1", discard: true}, false},
		{[]string{"--keep=remote", "tx1", "keymap"},
			resolveArgs{txid: "tx1", entity: "keymap", keep: "remote"}, false},
		{[]string{}, resolveArgs{}, true},
		{[]string{"tx1", "a", "b"}, resolveArgs{}, true},
		{[]string{"tx1", "--unknown"}, resolveArgs{}, true},
		{[]string{"tx1", "--keep"}, resolveArgs{}, true},
	}
	for _, c := range cases {
		got, err := parseResolveArgs(c.args)
		if (err != nil) != c.wantErr {
			t.Fatalf("%v: err=%v", c.args, err)
		}
		if err == nil && got != c.want {
			t.Fatalf("%v: got=%+v want=%+v", c.args, got, c.want)
		}
	}
}
