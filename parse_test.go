package main

import "testing"

// Cuerpos reducidos pero fieles a lo que publica cada fuente.

const bodyClaude = `## What's changed

- Added owner wildcard entries to managed settings
- Fixed a Bash permission bypass where a crafted command could hide itself
`

const bodyOpenCode = `## Core

### Improvements

- Simplified xAI login to a single device-code flow.

### Bugfixes

- Retried more transient provider errors.

## Desktop

### Bugfixes

- Fixed several right-to-left layout issues.

**Thank you to 1 community contributor:**
- @jamesmurdza:
  - fix(server): don't forward host directory (#40136)
`

const bodyGemini = `## What's Changed
* Changelog for v0.53.0-preview.0 by @gemini-cli-robot in https://github.com/g/c/pull/28507
* chore(release): bump version to 0.54.0-nightly by @gemini-cli-robot in https://github.com/g/c/pull/28510
* fix(core): rotate session ID on model fallback by @amelidev in https://github.com/g/c/pull/28469

**Full Changelog**: https://github.com/g/c/compare/v0.53.1...v0.54.0
`

const bodyCodex = `## Bug Fixes

- Apply safer automatic-review defaults for cyber-capable models. (#37057)

## Changelog

Full Changelog: https://github.com/openai/codex/compare/a...b

- #37057 [0.146] Backport safer cyber-model auto-review defaults @anp-oai
`

func TestParseClaude(t *testing.T) {
	secs := parseBody(bodyClaude, true)
	if len(secs) != 1 {
		t.Fatalf("esperaba 1 sección, obtuve %d: %+v", len(secs), secs)
	}
	if secs[0].Name != "" {
		t.Errorf("«What's changed» es genérico, debe quedar sin nombre; obtuve %q", secs[0].Name)
	}
	if len(secs[0].Changes) != 2 {
		t.Errorf("esperaba 2 cambios, obtuve %d", len(secs[0].Changes))
	}
}

func TestParseOpenCodeCortaEnPie(t *testing.T) {
	secs := parseBody(bodyOpenCode, true)
	if n := countChanges(secs); n != 3 {
		t.Fatalf("esperaba 3 cambios (el bloque de agradecimientos sobra), obtuve %d: %+v", n, secs)
	}
	// Dos "Bugfixes" en el mismo release deben quedar distinguibles por su padre.
	want := []string{"Core · Improvements", "Core · Bugfixes", "Desktop · Bugfixes"}
	for i, w := range want {
		if secs[i].Name != w {
			t.Errorf("sección %d = %q, esperada %q", i, secs[i].Name, w)
		}
	}
}

func TestHeading(t *testing.T) {
	cases := []struct {
		in    string
		name  string
		level int
		ok    bool
	}{
		{"## Core", "Core", 2, true},
		{"### Bug Fixes", "Bug Fixes", 3, true},
		{"#sin espacio", "", 0, false},
		{"# ", "", 0, false},
		{"texto", "", 0, false},
	}
	for _, c := range cases {
		n, l, ok := heading(c.in)
		if n != c.name || l != c.level || ok != c.ok {
			t.Errorf("heading(%q) = (%q,%d,%v), esperado (%q,%d,%v)",
				c.in, n, l, ok, c.name, c.level, c.ok)
		}
	}
}

func TestParseGeminiFiltraRuido(t *testing.T) {
	secs := parseBody(bodyGemini, true)
	if n := countChanges(secs); n != 1 {
		t.Fatalf("esperaba 1 cambio real de 3 líneas, obtuve %d: %+v", n, secs)
	}
	got := secs[0].Changes[0]
	want := "fix(core): rotate session ID on model fallback"
	if got != want {
		t.Errorf("atribución mal recortada:\n obtuve %q\n espero %q", got, want)
	}
	// Sin filtro deben volver las tres.
	if n := countChanges(parseBody(bodyGemini, false)); n != 3 {
		t.Errorf("--raw debe conservar las 3 líneas, obtuve %d", n)
	}
}

func TestParseCodexDescartaSeccionVacia(t *testing.T) {
	secs := parseBody(bodyCodex, true)
	if len(secs) != 1 || secs[0].Name != "Bug Fixes" {
		t.Fatalf("la sección «Changelog» solo tiene referencias a PR y debe desaparecer: %+v", secs)
	}
}

func TestStripAttribution(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fix: algo by @user in https://x/y/1", "fix: algo"},
		{"añade soporte para by @mention sin url", "añade soporte para by @mention sin url"},
		{"sin atribución", "sin atribución"},
	}
	for _, c := range cases {
		if got := stripAttribution(c.in); got != c.want {
			t.Errorf("stripAttribution(%q) = %q, esperado %q", c.in, got, c.want)
		}
	}
}

func TestBullet(t *testing.T) {
	if _, _, ok := bullet("**Thank you to 1 contributor:**"); ok {
		t.Error("«**negrita**» no es una viñeta")
	}
	text, indent, ok := bullet("  - anidado")
	if !ok || text != "anidado" || indent != 2 {
		t.Errorf("viñeta anidada mal leída: %q %d %v", text, indent, ok)
	}
}
