package main

import "testing"

func TestReviewedMachineUserPaths_derives_one_path_per_claim(t *testing.T) {
	paths, err := reviewedMachineUserPaths(" kora/kora-account-deleter, kora/kora-eval ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"kora-account-deleter": "kora/app/kora-account-deleter", "kora-eval": "kora/app/kora-eval"}
	if len(paths) != len(want) || paths["kora-account-deleter"] != want["kora-account-deleter"] || paths["kora-eval"] != want["kora-eval"] {
		t.Fatalf("paths = %v", paths)
	}
	if empty, err := reviewedMachineUserPaths(""); err != nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
	for _, bad := range []string{"kora", "kora/../x", "Kora/deleter", "kora/a,kora/a"} {
		if _, err := reviewedMachineUserPaths(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
