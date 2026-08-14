package config

import "testing"

func TestApplyProfileExtraMounts(t *testing.T) {
	cfg := &UserConfig{
		ExtraMounts: []ExtraMount{
			{Host: "/host/existing", Container: "/container/existing"},
		},
	}

	// Profile with no ExtraMounts set (nil) should leave existing mounts untouched.
	ApplyProfile(cfg, ProfileFile{})
	if len(cfg.ExtraMounts) != 1 || cfg.ExtraMounts[0].Host != "/host/existing" {
		t.Fatalf("expected existing ExtraMounts to be preserved when profile has none, got: %+v", cfg.ExtraMounts)
	}

	// Profile with ExtraMounts set should replace wholesale.
	p := ProfileFile{
		ExtraMounts: []ExtraMount{
			{Host: "/host/a", Container: "/container/a", ReadOnly: true},
			{Host: "/host/b", Container: "/container/b"},
		},
	}
	ApplyProfile(cfg, p)
	if len(cfg.ExtraMounts) != 2 {
		t.Fatalf("expected 2 ExtraMounts after applying profile, got: %+v", cfg.ExtraMounts)
	}
	if cfg.ExtraMounts[0] != (ExtraMount{Host: "/host/a", Container: "/container/a", ReadOnly: true}) {
		t.Fatalf("unexpected first ExtraMount: %+v", cfg.ExtraMounts[0])
	}
	if cfg.ExtraMounts[1] != (ExtraMount{Host: "/host/b", Container: "/container/b"}) {
		t.Fatalf("unexpected second ExtraMount: %+v", cfg.ExtraMounts[1])
	}

	// Profile explicitly setting an empty (non-nil) slice should replace wholesale too.
	ApplyProfile(cfg, ProfileFile{ExtraMounts: []ExtraMount{}})
	if len(cfg.ExtraMounts) != 0 {
		t.Fatalf("expected ExtraMounts to be empty after applying empty profile slice, got: %+v", cfg.ExtraMounts)
	}
}
