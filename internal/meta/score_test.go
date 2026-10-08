package meta

import "testing"

func TestTitleScore(t *testing.T) {
	cases := []struct {
		a, b     string
		min, max int
	}{
		{"Hades", "hades", 100, 100},
		{"The Witcher III: Wild Hunt", "Witcher 3 - Wild Hunt", 100, 100},
		{"Pokémon Red", "Pokemon Red", 100, 100},
		{"Hades", "Hades II", 0, 59},
		{"Resident Evil 2", "Resident Evil 3", 0, 59},
		{"Portal 2", "Portal", 0, 59},
		{"Half-Life: Alyx", "Half Life Alyx", 100, 100},
		{"Dark Souls III", "Dark Souls: Remastered", 0, 80},
		{"Celeste", "Celest", 80, 99},
		{"Doom", "Minecraft", 0, 30},
		{"", "Hades", 0, 0},
	}
	for _, c := range cases {
		if got := TitleScore(c.a, c.b); got < c.min || got > c.max {
			t.Errorf("TitleScore(%q, %q) = %d, want %d..%d", c.a, c.b, got, c.min, c.max)
		}
	}
}
