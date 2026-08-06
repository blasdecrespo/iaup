package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cuánta historia pedimos por fuente.
//
// La latencia de la API de GitHub es lineal en per_page, y lo sigue siendo en
// las revalidaciones 304: pedir 50 releases de Codex cuesta ~10 s aunque el
// servidor acabe respondiendo "no ha cambiado". Medido, no supuesto.
//
// Así que la profundidad se pide, no se asume. `status` y ver un changelog se
// resuelven con 20; solo `search`, `--list` y un `diff` muy retrasado bajan a
// por el resto.
const (
	depthShallow = 20
	depthDeep    = 60
)

// Release es un release ya normalizado. Es la única forma en la que el resto
// del programa ve los datos: nada aguas abajo sabe que existe GitHub.
//
// SourceID y Source no se serializan: se rellenan al leer la caché. Repetir el
// nombre de la herramienta en 50 registros es peso muerto en disco.
type Release struct {
	SourceID string    `json:"-"`
	Source   string    `json:"-"`
	Tag      string    `json:"tag"`
	Version  string    `json:"version"`
	Date     time.Time `json:"date"`
	Pre      bool      `json:"pre,omitempty"`
	Body     string    `json:"body,omitempty"`
	URL      string    `json:"url"`
}

// ghRelease es exactamente el subconjunto de la respuesta de GitHub que usamos.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// cacheEntry es lo que guardamos en disco: los releases ya normalizados más el
// ETag que identifica la respuesta. Con el ETag, revalidar cuesta 0 peticiones
// del rate limit cuando nada ha cambiado (GitHub responde 304 y no lo cuenta).
//
// Guardar la respuesta cruda costaba 14 MB solo para Codex: la API devuelve un
// array `assets` con cada binario publicado por plataforma, que aquí no se usa
// jamás. Normalizar antes de escribir dejó ese fichero en ~2% del tamaño y
// eliminó una segunda pasada de json.Unmarshal en cada lectura.
type cacheEntry struct {
	ETag     string    `json:"etag"`
	Fetched  time.Time `json:"fetched"`
	Depth    int       `json:"depth"` // per_page con el que se pidió
	Releases []Release `json:"releases"`
}

type fetchOpts struct {
	TTL     time.Duration
	Depth   int
	NoCache bool
	Offline bool

	// Token resuelve la credencial, o es nil para ir anónimo. Es una función
	// y no una cadena a propósito: así solo se paga cuando de verdad se sale
	// a la red, y un acierto de caché no lanza ningún proceso.
	Token func() string
}

// Result acompaña cada fuente con su error propio: que Gemini esté caído no
// puede tumbar la tabla entera.
type Result struct {
	Source   Source
	Releases []Release
	Cached   bool
	Err      error
}

var client = &http.Client{Timeout: 20 * time.Second}

func cacheDir() string {
	if d := os.Getenv("IAUP_CACHE"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "iaup")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "iaup")
	}
	return filepath.Join(home, ".cache", "iaup")
}

var tokenEnv = []string{"IAUP_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"}

// lookupToken busca una credencial para la API de GitHub. Nunca la escribe en
// disco ni la imprime; solo viaja en la cabecera Authorization hacia
// api.github.com.
//
// Una variable de entorno es una decisión que has tomado tú. Preguntarle a
// `gh` por su token no lo es: sería usar en silencio la sesión que tengas
// abierta, que puede ser la del trabajo. Por eso hay que pedirlo con
// --gh-token.
//
// Ir sin credencial no rompe nada: baja el límite de 5000 a 60 peticiones por
// hora, y como las revalidaciones 304 no cuentan, sobra para estas fuentes.
func lookupToken(useGH bool) string {
	for _, k := range tokenEnv {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if !useGH {
		return ""
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fetchOne devuelve los releases de una fuente.
//
// Orden de preferencia, de barato a caro:
//  1. Caché fresca         -> 0 peticiones, ~0 ms.
//  2. Revalidación 304     -> 1 petición que no consume rate limit.
//  3. Descarga 200         -> 1 petición.
//  4. Caché caducada       -> si la red falla, servir lo viejo es mejor que fallar.
func fetchOne(s Source, o fetchOpts) ([]Release, bool, error) {
	if o.Depth == 0 {
		o.Depth = depthShallow
	}
	path := filepath.Join(cacheDir(), s.ID+".json")
	cached := readCache(path)
	deepEnough := cached != nil && cached.Depth >= o.Depth

	if deepEnough && !o.NoCache && time.Since(cached.Fetched) < o.TTL {
		return stamp(s, cached.Releases), true, nil
	}
	if o.Offline {
		if cached == nil {
			return nil, false, errors.New("sin caché y en modo offline")
		}
		return stamp(s, cached.Releases), true, nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", s.Repo, o.Depth)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "iaup/"+buildVersion)
	if o.Token != nil {
		if t := o.Token(); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	}
	// El ETag identifica una respuesta concreta, y per_page forma parte de
	// ella. Reenviarlo con otra profundidad solo garantiza un 200.
	if cached != nil && cached.ETag != "" && cached.Depth == o.Depth && !o.NoCache {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	// stale sirve la caché caducada cuando la alternativa es no dar nada.
	// Menos profunda de lo pedido también vale: recortada es mejor que vacía.
	//
	// Pero lo dice. Servir datos viejos en silencio esconde el motivo —una
	// credencial caducada, el límite agotado, la red caída— y quien mira la
	// tabla creería estar viendo lo último. Va por stderr para no ensuciar
	// la salida ni romper una tubería con --json.
	stale := func(err error) ([]Release, bool, error) {
		if cached != nil {
			fmt.Fprintf(os.Stderr, "%s%s: %v; sirviendo caché de hace %s%s\n",
				cDim, s.Name, err, relTime(time.Since(cached.Fetched).Seconds()), cReset)
			return stamp(s, cached.Releases), true, nil
		}
		return nil, false, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return stale(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		cached.Fetched = time.Now()
		writeCache(path, cached)
		return stamp(s, cached.Releases), true, nil

	case resp.StatusCode == http.StatusOK:
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return stale(err)
		}
		rel, err := decode(raw)
		if err != nil {
			return stale(err)
		}
		writeCache(path, &cacheEntry{
			ETag:     resp.Header.Get("ETag"),
			Fetched:  time.Now(),
			Depth:    o.Depth,
			Releases: rel,
		})
		return stamp(s, rel), false, nil

	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return stale(errors.New("límite de peticiones de GitHub agotado: exporta GH_TOKEN para subir de 60 a 5000/hora"))

	default:
		return stale(fmt.Errorf("GitHub devolvió %s", resp.Status))
	}
}

// stamp rellena los campos de procedencia que no se guardan en disco y aplica
// la regla de precompilación. Va aquí, en la lectura, para que valga por igual
// sobre una respuesta recién bajada y sobre una caché escrita hace semanas.
func stamp(s Source, rel []Release) []Release {
	for i := range rel {
		rel[i].SourceID, rel[i].Source = s.ID, s.Name
		rel[i].Pre = rel[i].Pre || looksPrerelease(rel[i].Version)
	}
	return rel
}

func decode(raw []byte) ([]Release, error) {
	var gh []ghRelease
	if err := json.Unmarshal(raw, &gh); err != nil {
		return nil, fmt.Errorf("respuesta ilegible: %w", err)
	}
	out := make([]Release, 0, len(gh))
	for _, g := range gh {
		if g.Draft {
			continue
		}
		v := firstVersion(g.TagName)
		if v == "" {
			v = strings.TrimPrefix(g.TagName, "v")
		}
		out = append(out, Release{
			Tag:     g.TagName,
			Version: v,
			Date:    g.PublishedAt,
			Pre:     g.Prerelease,
			Body:    g.Body,
			URL:     g.HTMLURL,
		})
	}
	return out, nil
}

func readCache(path string) *cacheEntry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c cacheEntry
	if json.Unmarshal(raw, &c) != nil || len(c.Releases) == 0 {
		return nil
	}
	return &c
}

// writeCache escribe y renombra: un Ctrl-C a mitad no deja caché corrupta.
func writeCache(path string, c *cacheEntry) {
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	raw, err := json.Marshal(c)
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

// fetchAll consulta todas las fuentes a la vez. El coste total es el de la
// fuente más lenta, no la suma. Cada goroutine escribe su propio índice, así
// que no hace falta candado ni canal de resultados.
func fetchAll(srcs []Source, o fetchOpts) []Result {
	out := make([]Result, len(srcs))
	var wg sync.WaitGroup
	for i, s := range srcs {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			rel, cached, err := fetchOne(s, o)
			out[i] = Result{Source: s, Releases: rel, Cached: cached, Err: err}
		}(i, s)
	}
	wg.Wait()
	return out
}

// pickStable devuelve el primer release publicado que no sea precompilación.
// Codex publica decenas de alphas con el cuerpo vacío entre versiones reales:
// sin este filtro, "la última versión" es siempre ruido.
func pickStable(rel []Release, includePre bool) (Release, bool) {
	for _, r := range rel {
		if r.Pre && !includePre {
			continue
		}
		if r.Date.IsZero() {
			continue
		}
		return r, true
	}
	return Release{}, false
}

func filterReleases(rel []Release, includePre bool) []Release {
	out := rel[:0:0]
	for _, r := range rel {
		if r.Pre && !includePre {
			continue
		}
		out = append(out, r)
	}
	return out
}
