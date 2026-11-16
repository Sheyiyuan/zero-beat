package main

import (
	"reflect"
	"testing"

	syncpkg "github.com/sheyiyuan/zero-beat/zero-beat-core/sync"
)

func TestSyncBackendArgument(t *testing.T) {
	for _, args := range [][]string{{"--backend", "gogit", "pull"}, {"pull", "--backend=gogit"}, {"pull", "--backend", "gogit"}} {
		rest, backend, err := parseSyncBackend(args)
		if err != nil || backend != syncpkg.BackendGoGit || !reflect.DeepEqual(rest, []string{"pull"}) {
			t.Fatal(rest, backend, err)
		}
	}
	for _, args := range [][]string{{"--backend"}, {"--backend="}, {"--backend", ""}, {"pull", "--backend", "unknown"}} {
		if _, _, err := parseSyncBackend(args); err == nil {
			t.Fatal("accepted", args)
		}
	}
}
