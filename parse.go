package main

import "strings"

// Section es un bloque de cambios con título. Un cuerpo sin títulos produce
// una sola sección con Name vacío: así el renderizador no tiene casos especiales.
type Section struct {
	Name    string   `json:"name,omitempty"`
	Changes []string `json:"changes"`
}

// Títulos que no aportan nada. Aparecen en casi todos los releases y solo
// añaden una línea de ruido antes de la lista.
var genericHeadings = map[string]bool{
	"what's changed": true, "whats changed": true, "what's new": true,
	"changelog": true, "release notes": true, "changes": true,
	"commits": true, "new contributors": true,
}

// Marcas que terminan el contenido útil. Todo lo que viene después es pie de
// página autogenerado: enlaces de comparación y listas de colaboradores.
var trailers = []string{
	"**full changelog**", "full changelog:", "**thank you to",
	"## new contributors", "**new contributors**",
}

// Prefijos de cambios que no son cambios para quien usa la herramienta:
// tareas de mantenimiento, versiones automáticas, dependencias.
var noisePrefixes = []string{
	"chore(", "chore:", "chore/", "ci(", "ci:", "build(", "build:",
	"test(", "test:", "tests(", "docs(", "docs:", "deps(", "deps:",
	"style(", "bump ", "release ", "release(", "release:",
}

var noiseContains = []string{
	"bump version", "changelog for v", "cherry-pick", "update dependency",
	"version packages", "[skip ci]",
}

// parseBody convierte el markdown de un release en secciones.
//
// Con clean=true descarta el ruido de mantenimiento y las atribuciones
// "by @autor in <url>". En fuentes como Gemini CLI eso reduce un volcado de
// 17 líneas de pull requests a los cambios que de verdad se notan.
func parseBody(body string, clean bool) []Section {
	var out []Section
	cur := Section{}
	inBullet := false
	parent := "" // título de nivel 1-2 vigente

	flush := func() {
		if len(cur.Changes) > 0 {
			out = append(out, cur)
		}
		cur = Section{}
	}

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			inBullet = false
			continue
		}
		if isTrailer(trimmed) {
			break
		}
		if h, level, ok := heading(trimmed); ok {
			flush()
			if genericHeadings[strings.ToLower(h)] {
				h = ""
			}
			// Un subtítulo hereda el contexto de su padre. Sin esto, OpenCode
			// produce dos secciones llamadas "Bugfixes" en el mismo release
			// (una bajo "Core", otra bajo "Desktop") y no se distinguen.
			if level >= 3 && parent != "" && h != "" {
				cur.Name = parent + " · " + h
			} else {
				cur.Name = h
				parent = h
			}
			inBullet = false
			continue
		}
		if text, indent, ok := bullet(line); ok {
			if clean {
				text = stripAttribution(text)
				if isNoise(text) {
					inBullet = false
					continue
				}
			}
			if text == "" {
				continue
			}
			if indent >= 2 && len(cur.Changes) > 0 {
				text = "  " + text // conserva un nivel de anidamiento
			}
			cur.Changes = append(cur.Changes, text)
			inBullet = true
			continue
		}
		// Continuación de la viñeta anterior: una frase partida en dos líneas
		// no debe quedar cortada a la mitad.
		if inBullet && len(cur.Changes) > 0 && line != trimmed {
			cur.Changes[len(cur.Changes)-1] += " " + trimmed
		} else {
			inBullet = false
		}
	}
	flush()
	return out
}

func heading(s string) (name string, level int, ok bool) {
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level >= len(s) || s[level] != ' ' {
		return "", 0, false
	}
	name = strings.Trim(strings.TrimSpace(s[level:]), " :*")
	if name == "" {
		return "", 0, false
	}
	return name, level, true
}

// bullet reconoce "- texto", "* texto" y "+ texto" y devuelve la sangría.
func bullet(line string) (text string, indent int, ok bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == '\t' {
			indent += 4
		} else {
			indent++
		}
		i++
	}
	if i >= len(line) {
		return "", 0, false
	}
	if c := line[i]; c != '-' && c != '*' && c != '+' {
		return "", 0, false
	}
	if i+1 >= len(line) || (line[i+1] != ' ' && line[i+1] != '\t') {
		return "", 0, false // "**negrita**", no una viñeta
	}
	return strings.TrimSpace(line[i+2:]), indent, true
}

func isTrailer(s string) bool {
	low := strings.ToLower(s)
	for _, t := range trailers {
		if strings.HasPrefix(low, t) {
			return true
		}
	}
	return false
}

// stripAttribution quita el " by @autor in <url>" que GitHub añade a las notas
// autogeneradas. Solo corta cuando el patrón completo está presente, para no
// mutilar una frase que mencione a alguien.
func stripAttribution(s string) string {
	i := strings.LastIndex(s, " by @")
	if i < 0 {
		return s
	}
	if !strings.Contains(s[i:], " in http") {
		return s
	}
	return strings.TrimSpace(s[:i])
}

func isNoise(s string) bool {
	if s == "" {
		return true
	}
	low := strings.ToLower(s)
	for _, p := range noisePrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	for _, c := range noiseContains {
		if strings.Contains(low, c) {
			return true
		}
	}
	// "#37057 [0.146] Backport ..." — referencia a un pull request, ya cubierta
	// por la sección de cambios reales del mismo release.
	if low[0] == '#' && len(low) > 1 && isDigit(low[1]) {
		return true
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return true
	}
	return false
}

func countChanges(secs []Section) int {
	n := 0
	for _, s := range secs {
		n += len(s.Changes)
	}
	return n
}
