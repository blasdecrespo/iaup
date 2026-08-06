package main

import (
	"testing"
	"time"
)

func TestParseFlagsPosicionLibre(t *testing.T) {
	for _, argv := range [][]string{
		{"claude", "--json"},
		{"--json", "claude"},
		{"claude", "-j"},
	} {
		var o opts
		rest, err := parseFlags(argv, &o)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if !o.json {
			t.Errorf("%v: no activó --json", argv)
		}
		if len(rest) != 1 || rest[0] != "claude" {
			t.Errorf("%v: posicionales = %v, esperaba [claude]", argv, rest)
		}
	}
}

func TestParseFlagsValores(t *testing.T) {
	var o opts
	rest, err := parseFlags([]string{"latest", "--since", "72h", "--ttl=30m", "-n", "5"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if o.since != 72*time.Hour || o.ttl != 30*time.Minute || o.limit != 5 {
		t.Errorf("valores mal leídos: since=%v ttl=%v limit=%d", o.since, o.ttl, o.limit)
	}
	if len(rest) != 1 || rest[0] != "latest" {
		t.Errorf("posicionales = %v", rest)
	}
}

func TestParseFlagsErrores(t *testing.T) {
	cases := [][]string{
		{"claude", "--xyz"},      // desconocida sin valor detrás: no debe pedir valor
		{"claude", "--xyz", "1"}, // desconocida con algo detrás: no debe tragárselo
		{"--json=si"},            // booleana con valor
		{"latest", "--since"},    // valor ausente
		{"latest", "--since", "manzana"},
		{"-n", "muchos"},
	}
	for _, argv := range cases {
		var o opts
		if _, err := parseFlags(argv, &o); err == nil {
			t.Errorf("%v: esperaba error, no hubo", argv)
		}
	}
	// El mensaje debe señalar la bandera, no inventar que falta un valor.
	var o opts
	_, err := parseFlags([]string{"claude", "--xyz"}, &o)
	if err == nil || !contains(err.Error(), "desconocida") {
		t.Errorf("error poco honesto para --xyz: %v", err)
	}
}

func TestParseFlagsSeparador(t *testing.T) {
	var o opts
	rest, err := parseFlags([]string{"search", "--", "--json"}, &o)
	if err != nil {
		t.Fatal(err)
	}
	if o.json {
		t.Error("tras -- nada es una bandera")
	}
	if len(rest) != 2 || rest[1] != "--json" {
		t.Errorf("posicionales = %v", rest)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
