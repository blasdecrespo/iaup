package main

import "testing"

func TestLookupTokenPrecedencia(t *testing.T) {
	t.Setenv("IAUP_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	// Sin nada en el entorno y sin --gh-token: anónimo. No debe preguntar a gh.
	if got := lookupToken(false); got != "" {
		t.Errorf("sin entorno y sin --gh-token debe ir anónimo, obtuve %q", got)
	}
	// La variable manda sobre gh, y no hace falta --gh-token para usarla.
	t.Setenv("GITHUB_TOKEN", "del-entorno")
	if got := lookupToken(false); got != "del-entorno" {
		t.Errorf("GITHUB_TOKEN = %q", got)
	}
	// Orden entre variables: IAUP_TOKEN gana.
	t.Setenv("GH_TOKEN", "medio")
	t.Setenv("IAUP_TOKEN", "primero")
	if got := lookupToken(true); got != "primero" {
		t.Errorf("IAUP_TOKEN debe ganar, obtuve %q", got)
	}
}
