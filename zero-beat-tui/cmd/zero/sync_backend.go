package main

import (
	"fmt"
	"strings"

	syncpkg "github.com/sheyiyuan/zero-beat-core/sync"
)

func parseSyncBackend(args []string) ([]string, syncpkg.Backend, error) {
	var out []string
	var backend syncpkg.Backend
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--backend" {
			i++
			if i >= len(args) || args[i] == "" {
				return nil, "", fmt.Errorf("--backend 缺少值")
			}
			backend = syncpkg.Backend(args[i])
		} else if strings.HasPrefix(arg, "--backend=") {
			backend = syncpkg.Backend(strings.TrimPrefix(arg, "--backend="))
			if backend == "" {
				return nil, "", fmt.Errorf("--backend 缺少值")
			}
		} else {
			out = append(out, arg)
		}
	}
	if err := syncpkg.ValidateBackend(backend); err != nil {
		return nil, "", err
	}
	return out, backend, nil
}
