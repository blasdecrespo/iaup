# iaup

Changelogs de los asistentes de código con IA, desde la terminal.

```
$ iaup
┌──────────────┬───────────┬─────────┬──────────────┬───────────┬──────────┐
│ Herramienta  │ Instalada │ Última  │ Estado       │ Publicada │ Cadencia │
├──────────────┼───────────┼─────────┼──────────────┼───────────┼──────────┤
│ Claude Code  │ 2.1.223   │ 2.1.223 │ al día       │ 11h       │ ~2d      │
│ OpenAI Codex │ -         │ 0.146.1 │ sin instalar │ 20h       │ ~5d      │
│ OpenCode     │ 1.17.13   │ 1.18.14 │ actualizar   │ 15h       │ ~1d      │
│ Gemini CLI   │ -         │ 0.54.0  │ sin instalar │ 10h       │ ~6d      │
│ Copilot CLI  │ -         │ 1.0.78  │ sin instalar │ 2d        │ ~3d      │
└──────────────┴───────────┴─────────┴──────────────┴───────────┴──────────┘
```

Cero dependencias. `go.mod` no tiene una sola línea `require`.

## Instalar

```bash
go build -o iaup . && install -m755 iaup ~/.local/bin/
```

Compilación cruzada, si hace falta: `GOOS=darwin GOARCH=arm64 go build -o iaup .`

## Uso

```
iaup                      estado de todas las herramientas
iaup diff                 qué te llevas si actualizas
iaup claude               último changelog de Claude Code
iaup claude 2.1.200       una versión concreta
iaup opencode -l          todas las versiones
iaup search sandbox       dónde y cuándo apareció "sandbox"
iaup latest --since 72h   todo lo publicado en tres días
```

| Bandera | Qué hace |
|---|---|
| `-j, --json` | salida JSON |
| `-m, --md` | salida markdown |
| `-l, --list` | lista de versiones |
| `-w, --web` | abrir en el navegador |
| `-n, --limit N` | limitar resultados |
| `--pre` | incluir precompilaciones (alpha, preview, nightly) |
| `--raw` | sin filtro de ruido |
| `--since 72h` | ventana de tiempo para `latest` |
| `--ttl 1h` | validez de la caché |
| `--no-cache` | forzar descarga |
| `--offline` | solo caché, sin red |

Las banderas van en cualquier posición: `iaup --json claude` y `iaup claude --json`
hacen lo mismo.

## Los tres comandos que justifican la herramienta

**`iaup diff`** — la pregunta real detrás de abrir un changelog no es "qué hay de
nuevo en el mundo", es "qué me llevo si actualizo". Detecta tu versión instalada,
la compara con la última y te da solo el hueco.

**`iaup search`** — "¿cuándo añadió Claude Code los hooks?" se responde una vez,
sobre todas las fuentes a la vez, en lugar de abrir seis changelogs. Solo mira
versiones estables; con `--pre` entran también las precompilaciones.

Cada búsqueda termina declarando cuánta historia ha mirado de verdad:

```
Cobertura: claude 60 (2mo) · codex 6 (22d) · opencode 56 (2mo) · gemini 12 (2mo) · copilot 19 (59d)
```

La ventana se pide en releases, no en días, y no todas las herramientas publican
igual: Codex saca nueve alphas por cada versión estable, así que los mismos 60
releases son dos meses de Claude Code y 22 días de Codex. Sin esa línea parecería
que Codex apenas corrige nada. Es un límite real, y se dice en vez de disimularlo.

Una precompilación no siempre viene marcada como tal: `superagent-ai/grok-cli`
publica sus `-rc` con `prerelease=false`. La bandera la rellena quien publica; el
sufijo de la etiqueta no miente. Se usan los dos.

**Filtro de ruido** — las notas autogeneradas de GitHub mezclan cambios reales con
tareas de mantenimiento. En Gemini CLI, un release de 16 líneas se queda en las 10
que se notan al usarlo: fuera `chore()`, `bump version`, `Changelog for v...` y las
atribuciones `by @autor in <url>`. Con `--raw` sale todo tal cual.

## Cómo consigue ir rápido

Todo lo de aquí abajo está medido en esta máquina, no estimado.

**La caché guarda datos normalizados, no la respuesta de GitHub.** La API devuelve
un array `assets` con cada binario publicado por plataforma. Para Codex eso son
14 MB de los que no se usa ni un byte. Guardando solo los ocho campos necesarios,
la caché entera pasó de 20 MB a 176 KB y `status` en caliente de 320 ms a 8 ms.

**Revalidación por ETag.** Cuando la caché caduca, se pregunta con
`If-None-Match`. Si nada cambió, GitHub responde 304 y **no lo cuenta contra el
límite de peticiones**.

**La profundidad se pide, no se asume.** La latencia de la API es lineal en
`per_page`, y lo sigue siendo en los 304: pedir 50 releases de Codex cuesta ~10 s
aunque el servidor acabe diciendo "no ha cambiado". `status` y ver un changelog se
resuelven con 20; solo `search`, `--list` y un `diff` muy retrasado bajan a por más.

**La versión instalada también se cachea, con el binario como clave.**
`opencode --version` tarda 1,0 s en arrancar 167 MB de ELF. Un binario cuyo tamaño
y fecha no han cambiado no puede haber cambiado de versión, así que preguntárselo
otra vez es tiempo tirado. Un `stat()` cuesta microsegundos.

**Todas las fuentes se consultan a la vez.** El coste es el de la más lenta, no la
suma.

| | frío | caliente |
|---|---|---|
| `iaup status` | 4,9 s | 8 ms |
| `iaup claude` | — | 7 ms |
| `iaup diff` | 3,3 s | 9 ms |

## Cuando algo falla

Si la red se cae y hay caché, sirve la caché en vez de fallar. Si una fuente da
error, las demás siguen saliendo. Un `Ctrl-C` a mitad de escritura no deja caché
corrupta: se escribe en un temporal y se renombra.

## Fuentes

| ID | Herramienta | Repositorio |
|---|---|---|
| `claude` | Claude Code | `anthropics/claude-code` |
| `codex` | OpenAI Codex | `openai/codex` |
| `opencode` | OpenCode | `anomalyco/opencode` |
| `gemini` | Gemini CLI | `google-gemini/gemini-cli` |
| `copilot` | Copilot CLI | `github/copilot-cli` |
| `grok` | Grok CLI (superagent) | `superagent-ai/grok-cli` |

> `grok` **no** es el Grok CLI oficial de xAI. Son dos herramientas distintas que
> instalan un ejecutable llamado `grok`. La de xAI no publica releases en GitHub:
> su changelog vive en un endpoint autenticado y en `~/.grok/CHANGELOG.md`. Por
> eso esta fila no detecta versión instalada — compararla con la de xAI diría
> "actualiza" enfrentando dos productos que no tienen nada que ver.

Todas se leen por la API de releases de GitHub. No hay lectores especiales por
herramienta: añadir una es **una fila** en la tabla `sources` de `source.go`.

```go
{"crush", "Crush", "charmbracelet/crush", "crush"},
```

Usa el repositorio canónico: uno renombrado devuelve 301 y gasta una petición de
más en cada llamada.

## Entorno

| Variable | Efecto |
|---|---|
| `GH_TOKEN` / `GITHUB_TOKEN` | sube el límite de GitHub de 60 a 5000 peticiones/hora |
| `IAUP_CACHE` | directorio de caché (por defecto `~/.cache/iaup`) |
| `NO_COLOR` | desactiva el color |

Si no hay ninguna de las dos primeras, se intenta `gh auth token`, y solo en el
momento de salir a la red. El token nunca se escribe en disco.

## Desarrollo

```bash
go test ./...
go vet ./...
```
