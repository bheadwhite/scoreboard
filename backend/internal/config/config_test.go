package config

import (
	"os"
	"testing"
)

// make exports an included KEY="value" with the quotes attached, and a value
// already in the environment wins over the dotenv file. A quoted APP_URL
// therefore produced redirect targets like `"http://localhost:3000"/`, which
// looks like an auth bug and is not one.
func TestEnvStripsSurroundingQuotes(t *testing.T) {
	cases := map[string]string{
		`"http://localhost:3000"`: "http://localhost:3000",
		`'http://localhost:3000'`: "http://localhost:3000",
		`http://localhost:3000`:   "http://localhost:3000",
		`"`:                       `"`,
		``:                        "fallback",
		`"unbalanced`:             `"unbalanced`,
		`say "hi"`:                `say "hi"`,
	}
	for raw, want := range cases {
		t.Setenv("SCOREBOARD_TEST_VALUE", raw)
		if got := Env("SCOREBOARD_TEST_VALUE", "fallback"); got != want {
			t.Errorf("Env(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestEnvFallsBackWhenUnset(t *testing.T) {
	os.Unsetenv("SCOREBOARD_TEST_MISSING")
	if got := Env("SCOREBOARD_TEST_MISSING", "def"); got != "def" {
		t.Errorf("expected the default, got %q", got)
	}
}
