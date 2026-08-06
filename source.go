package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Source es una herramienta cuyo changelog seguimos.
//
// Toda fuente se lee por la API de releases de GitHub. No hay lectores
// especiales por herramienta: una fuente nueva es una fila más en esta tabla.
//
// Repo debe ser el repositorio canónico. Un repo renombrado devuelve 301 y
// gasta una petición de más en cada llamada.
type Source struct {
	ID   string // nombre en la línea de comandos
	Name string // nombre para humanos
	Repo string // owner/repo canónico en GitHub
	Bin  string // ejecutable con el que detectar la versión instalada
}

var sources = []Source{
	{"claude", "Claude Code", "anthropics/claude-code", "claude"},
	{"codex", "OpenAI Codex", "openai/codex", "codex"},
	{"opencode", "OpenCode", "anomalyco/opencode", "opencode"},
	{"gemini", "Gemini CLI", "google-gemini/gemini-cli", "gemini"},
	{"copilot", "Copilot CLI", "github/copilot-cli", "copilot"},
}

func (s Source) WebURL() string { return "https://github.com/" + s.Repo + "/releases" }

func findSource(id string) (Source, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// installedVersion ejecuta `<bin> --version` y extrae el primer número de
// versión de la salida. Los formatos difieren ("2.1.223 (Claude Code)",
// "1.17.13"), el primer semver de la salida es el denominador común.
//
// Devuelve "" si el binario no está, falla o no imprime nada reconocible.
func installedVersion(bin string) string {
	if bin == "" {
		return ""
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return firstVersion(string(out))
}

// instEntry recuerda qué versión imprimió un binario concreto.
//
// La clave de invalidación es el propio fichero: tamaño y fecha. Un binario
// que no ha cambiado no puede haber cambiado de versión, así que preguntárselo
// otra vez es tiempo tirado. `opencode --version` tarda un segundo entero
// (167 MB de ELF); un stat() tarda microsegundos.
//
// Seen acota el error si algún día un lanzador no cambia al actualizarse.
type instEntry struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Version string    `json:"version"`
	Seen    time.Time `json:"seen"`
}

const instTTL = 24 * time.Hour

// installedAll devuelve la versión instalada de cada fuente. Consulta el disco
// primero y solo lanza procesos para los binarios que hayan cambiado, en
// paralelo entre sí.
func installedAll(srcs []Source, noCache bool) []string {
	cache := readInstCache()
	got := make([]string, len(srcs))
	stats := make([]os.FileInfo, len(srcs))
	var ask []int

	for i, s := range srcs {
		path, err := exec.LookPath(s.Bin)
		if err != nil {
			continue // no instalado: ni caché ni proceso
		}
		fi, err := os.Stat(path) // sigue el enlace: mide el binario real
		if err != nil {
			continue
		}
		stats[i] = fi
		if e, ok := cache[s.Bin]; ok && !noCache &&
			e.Size == fi.Size() && e.ModTime.Equal(fi.ModTime()) &&
			time.Since(e.Seen) < instTTL {
			got[i] = e.Version
			continue
		}
		ask = append(ask, i)
	}

	done := make(chan int, len(ask))
	for _, i := range ask {
		go func(i int, bin string) {
			got[i] = installedVersion(bin)
			done <- i
		}(i, srcs[i].Bin)
	}
	for range ask {
		<-done
	}

	if len(ask) > 0 {
		now := time.Now()
		for _, i := range ask {
			if stats[i] == nil {
				continue
			}
			cache[srcs[i].Bin] = instEntry{
				Size: stats[i].Size(), ModTime: stats[i].ModTime(),
				Version: got[i], Seen: now,
			}
		}
		writeInstCache(cache)
	}
	return got
}

func instCachePath() string { return filepath.Join(cacheDir(), "installed.json") }

func readInstCache() map[string]instEntry {
	m := map[string]instEntry{}
	raw, err := os.ReadFile(instCachePath())
	if err != nil {
		return m
	}
	if json.Unmarshal(raw, &m) != nil {
		return map[string]instEntry{}
	}
	return m
}

func writeInstCache(m map[string]instEntry) {
	path := instCachePath()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

func sourceIDs() string {
	ids := make([]string, len(sources))
	for i, s := range sources {
		ids[i] = s.ID
	}
	return strings.Join(ids, ", ")
}
