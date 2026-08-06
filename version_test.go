package main

import "testing"

func TestFirstVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		// Etiquetas reales de las fuentes. El prefijo "v" rompió el parser una
		// vez: leía "v1.18.14" como "8.14".
		{"v1.18.14", "1.18.14"},
		{"rust-v0.146.1", "0.146.1"},
		{"v0.54.0", "0.54.0"},
		{"v2.1.223", "2.1.223"},
		{"v1.0.78-5", "1.0.78-5"},
		{"v0.55.0-preview.1", "0.55.0-preview.1"},
		// Salidas reales de `--version`.
		{"2.1.223 (Claude Code)", "2.1.223"},
		{"1.17.13", "1.17.13"},
		{"codex-cli 0.92.0\n", "0.92.0"},
		// Nada que extraer.
		{"2026-08-03", ""},
		{"", ""},
		{"sin version", ""},
		{"7", ""},
	}
	for _, c := range cases {
		if got := firstVersion(c.in); got != c.want {
			t.Errorf("firstVersion(%q) = %q, esperado %q", c.in, got, c.want)
		}
	}
}

func TestCmpVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int // signo esperado
	}{
		{"1.18.14", "1.17.13", +1},
		{"1.17.13", "1.18.14", -1},
		{"2.1.223", "2.1.223", 0},
		{"0.10.0", "0.9.0", +1}, // numérico, no alfabético
		{"1.0.0", "1.0.0-alpha", +1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"2.0", "2.0.0", 0},
		{"", "1.0.0", -1},
	}
	for _, c := range cases {
		got := cmpVersion(c.a, c.b)
		if sign(got) != c.want {
			t.Errorf("cmpVersion(%q,%q) = %d, esperado signo %d", c.a, c.b, got, c.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func TestLooksPrerelease(t *testing.T) {
	pre := []string{
		"1.1.5-rc7", // grok-cli las publica con prerelease=false
		"0.147.0-alpha.13", "0.55.0-preview.1", "0.54.0-nightly.20260722",
		"2.0.0-beta", "1.0.0-dev", "3.1.0-canary.2",
	}
	for _, v := range pre {
		if !looksPrerelease(v) {
			t.Errorf("%q debería contar como precompilación", v)
		}
	}
	estables := []string{
		"1.0.78-5", // Copilot numera así sus publicaciones reales
		"2.1.223", "0.146.1", "1.18.14", "",
	}
	for _, v := range estables {
		if looksPrerelease(v) {
			t.Errorf("%q es estable, no precompilación", v)
		}
	}
}

func TestRelTime(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{30, "ahora"}, {600, "10m"}, {7200, "2h"},
		{86400 * 3, "3d"}, {86400 * 90, "3mo"}, {86400 * 400, "1y"},
	}
	for _, c := range cases {
		if got := relTime(c.sec); got != c.want {
			t.Errorf("relTime(%v) = %q, esperado %q", c.sec, got, c.want)
		}
	}
}
