package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var buildVersion = "dev"

type opts struct {
	json    bool
	md      bool
	list    bool
	web     bool
	pre     bool
	raw     bool
	noCache bool
	offline bool
	color   bool
	noColor bool
	ghToken bool
	ttl     time.Duration
	since   time.Duration
	limit   int

	// resolveToken se evalúa una sola vez, y solo si alguien sale a la red.
	resolveToken func() string
}

func main() {
	o := opts{ttl: time.Hour, since: 24 * time.Hour, limit: 0}
	args, err := parseFlags(os.Args[1:], &o)
	if err != nil {
		die(err)
	}
	initColor(o.color, o.noColor)
	o.resolveToken = sync.OnceValue(func() string { return lookupToken(o.ghToken) })

	cmd := "status"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "help", "-h", "--help":
		usage(os.Stdout)
	case "version", "-v", "--version":
		fmt.Println("iaup", buildVersion)
	case "sources":
		cmdSources()
	case "status":
		err = cmdStatus(o)
	case "latest":
		err = cmdLatest(o)
	case "diff":
		err = cmdDiff(o, args)
	case "search":
		if len(args) == 0 {
			die(fmt.Errorf("search necesita un término: iaup search hooks"))
		}
		err = cmdSearch(o, strings.Join(args, " "))
	default:
		s, ok := findSource(cmd)
		if !ok {
			die(fmt.Errorf("fuente desconocida %q; disponibles: %s", cmd, sourceIDs()))
		}
		ver := ""
		if len(args) > 0 {
			ver = args[0]
		}
		err = cmdShow(o, s, ver)
	}
	if err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "%siaup:%s %v\n", cRed, cReset, err)
	os.Exit(1)
}

func (o opts) fetchAt(depth int) fetchOpts {
	return fetchOpts{
		TTL: o.ttl, Depth: depth, NoCache: o.noCache,
		Offline: o.offline, Token: o.resolveToken,
	}
}

func (o opts) fetch() fetchOpts { return o.fetchAt(depthShallow) }

// ---------- comandos ----------

func cmdShow(o opts, s Source, ver string) error {
	if o.web && ver == "" {
		return openURL(s.WebURL())
	}
	// Listar versiones o pedir una concreta necesita historia; ver la última
	// no. La ventana corta es varias veces más rápida.
	depth := depthShallow
	if o.list || ver != "" {
		depth = depthDeep
	}
	rel, _, err := fetchOne(s, o.fetchAt(depth))
	if err != nil {
		return err
	}
	if len(rel) == 0 {
		return fmt.Errorf("%s no tiene releases publicados", s.Name)
	}
	if o.list {
		return listVersions(o, s, rel)
	}

	var r Release
	if ver != "" {
		want := firstVersion(ver)
		found := false
		for _, c := range rel {
			if c.Version == want || c.Tag == ver {
				r, found = c, true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s no tiene la versión %s entre los últimos %d releases", s.Name, ver, len(rel))
		}
	} else {
		var ok bool
		if r, ok = pickStable(rel, o.pre); !ok {
			return fmt.Errorf("%s solo publica precompilaciones; usa --pre", s.Name)
		}
	}
	if o.web {
		return openURL(r.URL)
	}

	secs := parseBody(r.Body, !o.raw)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	switch {
	case o.json:
		return emitJSON(out, toJSON(r, secs))
	case o.md:
		printMarkdown(out, r, secs)
	default:
		printRelease(out, r, secs)
	}
	return nil
}

func listVersions(o opts, s Source, rel []Release) error {
	rel = filterReleases(rel, o.pre)
	if o.limit > 0 && len(rel) > o.limit {
		rel = rel[:o.limit]
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if o.json {
		type row struct {
			Version    string    `json:"version"`
			Tag        string    `json:"tag"`
			ReleasedAt time.Time `json:"released_at"`
			Prerelease bool      `json:"prerelease"`
		}
		rows := make([]row, len(rel))
		for i, r := range rel {
			rows[i] = row{r.Version, r.Tag, r.Date, r.Pre}
		}
		return emitJSON(out, rows)
	}
	for _, r := range rel {
		mark := ""
		if r.Pre {
			mark = cYellow + " pre" + cReset
		}
		fmt.Fprintf(out, "%-16s %s%s%s%s\n", r.Version,
			cDim, r.Date.Local().Format("2006-01-02"), cReset, mark)
	}
	return nil
}

func cmdStatus(o opts) error {
	if o.web {
		return openURL("https://github.com/blasdecrespo/iaup")
	}
	res := fetchAll(sources, o.fetch())
	inst := installedAll(sources, o.noCache)

	type stat struct {
		ID        string    `json:"source"`
		Name      string    `json:"name"`
		Installed string    `json:"installed,omitempty"`
		Latest    string    `json:"latest,omitempty"`
		Date      time.Time `json:"released_at"`
		Cadence   string    `json:"cadence,omitempty"`
		State     string    `json:"state"`
		Err       string    `json:"error,omitempty"`
	}
	stats := make([]stat, len(res))
	for i, r := range res {
		st := stat{ID: r.Source.ID, Name: r.Source.Name, Installed: inst[i], State: "desconocido"}
		if r.Err != nil {
			st.Err = r.Err.Error()
			st.State = "error"
			stats[i] = st
			continue
		}
		latest, ok := pickStable(r.Releases, o.pre)
		if !ok {
			stats[i] = st
			continue
		}
		st.Latest, st.Date = latest.Version, latest.Date
		st.Cadence = cadence(filterReleases(r.Releases, o.pre))
		switch {
		case st.Installed == "":
			st.State = "sin instalar"
		case cmpVersion(st.Installed, st.Latest) >= 0:
			st.State = "al día"
		default:
			st.State = "actualizar"
		}
		stats[i] = st
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if o.json {
		return emitJSON(out, stats)
	}

	head := []string{"Herramienta", "Instalada", "Última", "Estado", "Publicada", "Cadencia"}
	var rows, plain [][]string
	for _, s := range stats {
		inst, latest, state, pub, cad := s.Installed, s.Latest, s.State, "-", s.Cadence
		if inst == "" {
			inst = "-"
		}
		if latest == "" {
			latest = "-"
		}
		if cad == "" {
			cad = "-"
		}
		if !s.Date.IsZero() {
			pub = relTime(time.Since(s.Date).Seconds())
		}
		color := cDim
		switch state {
		case "al día":
			color = cGreen
		case "actualizar":
			color = cYellow
		case "error":
			color = cRed
		}
		plain = append(plain, []string{s.Name, inst, latest, state, pub, cad})
		rows = append(rows, []string{
			cBold + s.Name + cReset, inst, latest,
			color + state + cReset, pub, cDim + cad + cReset,
		})
	}
	printTable(out, head, rows, plain)
	for _, s := range stats {
		if s.Err != "" {
			fmt.Fprintf(out, "%s%s: %s%s\n", cRed, s.Name, s.Err, cReset)
		}
	}
	return nil
}

// cadence estima cada cuánto publica una herramienta, promediando los huecos
// entre los últimos 10 releases. Es la señal que dice si "hace 3 días" es
// normal o es que el proyecto está parado.
func cadence(rel []Release) string {
	var dates []time.Time
	for _, r := range rel {
		if !r.Date.IsZero() {
			dates = append(dates, r.Date)
		}
		if len(dates) == 11 {
			break
		}
	}
	if len(dates) < 2 {
		return ""
	}
	total := dates[0].Sub(dates[len(dates)-1]).Seconds()
	return "~" + relTime(total/float64(len(dates)-1))
}

func cmdLatest(o opts) error {
	res := fetchAll(sources, o.fetch())
	cut := time.Now().Add(-o.since)

	var recent []Release
	for _, r := range res {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "%s%s: %v%s\n", cRed, r.Source.Name, r.Err, cReset)
			continue
		}
		for _, rel := range filterReleases(r.Releases, o.pre) {
			if rel.Date.After(cut) {
				recent = append(recent, rel)
			}
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].Date.After(recent[j].Date) })
	if o.limit > 0 && len(recent) > o.limit {
		recent = recent[:o.limit]
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if o.json {
		rows := make([]jsonRelease, len(recent))
		for i, r := range recent {
			rows[i] = toJSON(r, parseBody(r.Body, !o.raw))
		}
		return emitJSON(out, rows)
	}
	if len(recent) == 0 {
		fmt.Fprintf(out, "%sSin releases en las últimas %s. Prueba --since 72h.%s\n",
			cDim, o.since, cReset)
		return nil
	}
	for i, r := range recent {
		if i > 0 {
			fmt.Fprintln(out)
		}
		secs := parseBody(r.Body, !o.raw)
		if o.md {
			printMarkdown(out, r, secs)
		} else {
			printRelease(out, r, secs)
		}
	}
	return nil
}

// cmdDiff muestra lo que ha cambiado entre la versión instalada y la última.
// Es la pregunta real detrás de abrir un changelog: no "qué hay de nuevo en
// el mundo", sino "qué me llevo si actualizo".
func cmdDiff(o opts, ids []string) error {
	srcs := sources
	if len(ids) > 0 {
		srcs = nil
		for _, id := range ids {
			s, ok := findSource(id)
			if !ok {
				return fmt.Errorf("fuente desconocida %q; disponibles: %s", id, sourceIDs())
			}
			srcs = append(srcs, s)
		}
	}
	inst := installedAll(srcs, o.noCache)
	res := fetchAll(srcs, o.fetch())

	// Si la versión instalada es más vieja que el release más antiguo que
	// trajimos, el hueco no cabe en la ventana corta y estaríamos mintiendo
	// por omisión. Solo entonces se baja a por más historia.
	var deep []Source
	for i, r := range res {
		if r.Err != nil || inst[i] == "" || len(r.Releases) == 0 {
			continue
		}
		if cmpVersion(r.Releases[len(r.Releases)-1].Version, inst[i]) > 0 {
			deep = append(deep, r.Source)
		}
	}
	if len(deep) > 0 {
		for _, d := range fetchAll(deep, o.fetchAt(depthDeep)) {
			for i := range res {
				if res[i].Source.ID == d.Source.ID && d.Err == nil {
					res[i] = d
				}
			}
		}
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	type gap struct {
		Source    string        `json:"source"`
		Name      string        `json:"name"`
		Installed string        `json:"installed"`
		Latest    string        `json:"latest"`
		Behind    int           `json:"behind"`
		Releases  []jsonRelease `json:"releases"`
	}
	var gaps []gap
	printed := 0

	for i, r := range res {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "%s%s: %v%s\n", cRed, r.Source.Name, r.Err, cReset)
			continue
		}
		have := inst[i]
		if have == "" {
			if len(ids) > 0 {
				fmt.Fprintf(os.Stderr, "%s%s no está instalado%s\n", cDim, r.Source.Name, cReset)
			}
			continue
		}
		rel := filterReleases(r.Releases, o.pre)
		latest, ok := pickStable(rel, o.pre)
		if !ok {
			continue
		}
		var newer []Release
		for _, c := range rel {
			if cmpVersion(c.Version, have) > 0 {
				newer = append(newer, c)
			}
		}
		if len(newer) == 0 {
			if !o.json {
				fmt.Fprintf(out, "%s%s %s: al día%s\n", cGreen, r.Source.Name, have, cReset)
			}
			continue
		}
		sort.Slice(newer, func(a, b int) bool { return newer[a].Date.After(newer[b].Date) })
		if o.limit > 0 && len(newer) > o.limit {
			newer = newer[:o.limit]
		}

		if o.json {
			g := gap{r.Source.ID, r.Source.Name, have, latest.Version, len(newer), nil}
			for _, c := range newer {
				g.Releases = append(g.Releases, toJSON(c, parseBody(c.Body, !o.raw)))
			}
			gaps = append(gaps, g)
			continue
		}
		if printed > 0 {
			fmt.Fprintln(out)
		}
		printed++
		fmt.Fprintf(out, "%s%s%s  %s%s%s → %s%s%s  %s(%d versiones)%s\n",
			cBold, r.Source.Name, cReset,
			cYellow, have, cReset, cGreen, latest.Version, cReset,
			cDim, len(newer), cReset)
		for _, c := range newer {
			fmt.Fprintln(out)
			secs := parseBody(c.Body, !o.raw)
			if o.md {
				printMarkdown(out, c, secs)
			} else {
				printRelease(out, c, secs)
			}
		}
	}
	if o.json {
		if gaps == nil {
			gaps = []gap{}
		}
		return emitJSON(out, gaps)
	}
	if printed == 0 && len(ids) == 0 {
		fmt.Fprintf(out, "%sNada que actualizar.%s\n", cDim, cReset)
	}
	return nil
}

// cmdSearch responde a "¿cuándo apareció esto?" sin abrir cinco changelogs.
func cmdSearch(o opts, term string) error {
	res := fetchAll(sources, o.fetchAt(depthDeep)) // buscar sin historia no es buscar
	needle := strings.ToLower(term)

	type hit struct {
		Source     string    `json:"source"`
		Name       string    `json:"name"`
		Version    string    `json:"version"`
		ReleasedAt time.Time `json:"released_at"`
		Section    string    `json:"section,omitempty"`
		Change     string    `json:"change"`
		URL        string    `json:"url"`
	}
	var hits []hit
	for _, r := range res {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "%s%s: %v%s\n", cRed, r.Source.Name, r.Err, cReset)
			continue
		}
		for _, rel := range filterReleases(r.Releases, o.pre) {
			for _, sec := range parseBody(rel.Body, !o.raw) {
				for _, c := range sec.Changes {
					if strings.Contains(strings.ToLower(c), needle) {
						hits = append(hits, hit{r.Source.ID, r.Source.Name, rel.Version,
							rel.Date, sec.Name, strings.TrimSpace(c), rel.URL})
					}
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ReleasedAt.After(hits[j].ReleasedAt) })
	limit := o.limit
	if limit == 0 {
		limit = 40
	}
	total := len(hits)
	if total > limit {
		hits = hits[:limit]
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if o.json {
		if hits == nil {
			hits = []hit{}
		}
		return emitJSON(out, hits)
	}
	if total == 0 {
		fmt.Fprintf(out, "%sSin coincidencias para %q en los últimos %d releases de cada fuente.%s\n",
			cDim, term, depthDeep, cReset)
		return nil
	}
	width := wrapWidth()
	for _, h := range hits {
		fmt.Fprintf(out, "%s%s %s%s %s%s%s\n", cBold, h.Name, h.Version, cReset,
			cDim, h.ReleasedAt.Local().Format("2006-01-02"), cReset)
		fmt.Fprintln(out, wrap(highlight(h.Change, term), "  • ", "    ", width))
	}
	if total > len(hits) {
		fmt.Fprintf(out, "\n%s%d coincidencias más; usa -n %d para verlas.%s\n",
			cDim, total-len(hits), total, cReset)
	}
	fmt.Fprintf(out, "\n%sCobertura: %s%s\n", cDim, coverage(res, o.pre), cReset)
	return nil
}

// coverage dice cuánta historia ha mirado de verdad en cada fuente.
//
// La ventana se pide en releases, no en días, y no todas las herramientas
// publican al mismo ritmo ni con la misma proporción de precompilaciones:
// Codex saca nueve alphas por cada versión estable, así que los mismos 60
// releases son 70 días de Claude Code y 22 de Codex. Buscar sobre ventanas
// tan desiguales y no decirlo es mentir por omisión: parecería que Codex
// apenas corrige nada.
func coverage(res []Result, includePre bool) string {
	var parts []string
	for _, r := range res {
		if r.Err != nil {
			continue
		}
		rel := filterReleases(r.Releases, includePre)
		if len(rel) == 0 {
			continue
		}
		span := relTime(rel[0].Date.Sub(rel[len(rel)-1].Date).Seconds())
		parts = append(parts, fmt.Sprintf("%s %d (%s)", r.Source.ID, len(rel), span))
	}
	return strings.Join(parts, " · ")
}

func highlight(s, term string) string {
	if cReset == "" {
		return s
	}
	low, needle := strings.ToLower(s), strings.ToLower(term)
	var b strings.Builder
	for {
		i := strings.Index(low, needle)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(cYellow + cBold + s[i:i+len(needle)] + cReset)
		s, low = s[i+len(needle):], low[i+len(needle):]
	}
}

func cmdSources() {
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, s := range sources {
		fmt.Fprintf(out, "%s%-10s%s %-14s %s%s%s\n",
			cBold, s.ID, cReset, s.Name, cDim, s.Repo, cReset)
	}
}

func openURL(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	path, err := exec.LookPath(cmd)
	if err != nil {
		fmt.Println(url) // sin navegador: imprimir la URL sigue siendo útil
		return nil
	}
	return exec.Command(path, append(args, url)...).Start()
}

// ---------- flags ----------

// parseFlags acepta banderas en cualquier posición: `iaup --json claude` y
// `iaup claude --json` hacen lo mismo. El paquete flag de la biblioteca
// estándar se detiene en el primer argumento posicional, y eso obliga a quien
// usa la herramienta a recordar un orden que no aporta nada.
var takesValue = map[string]bool{
	"ttl": true, "since": true, "n": true, "limit": true,
}

func parseFlags(argv []string, o *opts) ([]string, error) {
	bools := map[string]*bool{
		"json": &o.json, "j": &o.json,
		"md": &o.md, "m": &o.md,
		"list": &o.list, "l": &o.list,
		"web": &o.web, "w": &o.web,
		"pre": &o.pre, "raw": &o.raw,
		"no-cache": &o.noCache, "offline": &o.offline,
		"color": &o.color, "no-color": &o.noColor,
		"gh-token": &o.ghToken,
	}
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			rest = append(rest, argv[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' || a == "-" {
			rest = append(rest, a)
			continue
		}
		// "-v" y "-h" se tratan como comandos para poder escribir `iaup -v`.
		if a == "-v" || a == "--version" || a == "-h" || a == "--help" {
			rest = append(rest, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		val := ""
		hasVal := false
		if k := strings.IndexByte(name, '='); k >= 0 {
			name, val, hasVal = name[:k], name[k+1:], true
		}
		if p, ok := bools[name]; ok {
			if hasVal {
				return nil, fmt.Errorf("--%s no lleva valor", name)
			}
			*p = true
			continue
		}
		// Reconocer la bandera antes de consumir su valor: si no, `--xyz` se
		// come el argumento siguiente y el error acusa a la víctima.
		if !takesValue[name] {
			return nil, fmt.Errorf("bandera desconocida %q (prueba: iaup help)", a)
		}
		if !hasVal {
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--%s necesita un valor", name)
			}
			i++
			val = argv[i]
		}
		var err error
		switch name {
		case "ttl":
			o.ttl, err = time.ParseDuration(val)
		case "since":
			o.since, err = time.ParseDuration(val)
		case "n", "limit":
			o.limit, err = strconv.Atoi(val)
		}
		if err != nil {
			return nil, fmt.Errorf("valor inválido para --%s: %v", name, err)
		}
	}
	return rest, nil
}

func usage(w *os.File) {
	fmt.Fprintf(w, `iaup %s — changelogs de asistentes de código con IA

USO
  iaup [comando] [banderas]

COMANDOS
  status              tabla con versión instalada, última y estado (por defecto)
  <fuente> [versión]  changelog de una herramienta
  diff [fuente...]    qué cambia entre tu versión instalada y la última
  latest              releases publicados en las últimas 24 horas
  search <término>    busca en los changelogs de todas las fuentes
  sources             lista las fuentes
  version, help

FUENTES
  %s

BANDERAS
  -j, --json          salida JSON
  -m, --md            salida markdown
  -l, --list          lista de versiones (con <fuente>)
  -w, --web           abrir en el navegador
  -n, --limit N       limitar resultados
      --pre           incluir precompilaciones (alpha, preview, nightly)
      --raw           sin filtro de ruido (chore, ci, bump, atribuciones)
      --since 72h     ventana de tiempo para latest
      --ttl 1h        validez de la caché
      --no-cache      forzar descarga
      --offline       solo caché, sin red
      --gh-token      usar la credencial de 'gh auth token'
      --color / --no-color

EJEMPLOS
  iaup                        estado de todas las herramientas
  iaup diff                   lo que te llevas si actualizas
  iaup claude                 último changelog de Claude Code
  iaup claude 2.1.200         una versión concreta
  iaup opencode -l            todas las versiones
  iaup search sandbox         dónde y cuándo apareció "sandbox"
  iaup latest --since 72h     todo lo publicado en 3 días

CREDENCIALES
  Por defecto va anónimo: 60 peticiones/hora, que sobran porque las
  revalidaciones 304 no cuentan contra el límite. Con credencial son 5000.

  Se usa la primera que exista, y nunca se escribe en disco:
    1. IAUP_TOKEN, GH_TOKEN o GITHUB_TOKEN
    2. 'gh auth token', solo si pasas --gh-token

  A gh no se le pregunta por su cuenta a propósito: usaría en silencio la
  sesión que tengas abierta, que puede no ser la que quieres para esto.

ENTORNO
  IAUP_CACHE                directorio de caché (por defecto ~/.cache/iaup)
  NO_COLOR                  desactiva el color
`, buildVersion, sourceIDs())
}
