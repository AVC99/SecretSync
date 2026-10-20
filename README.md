# secretsync

CLI multiplataforma (macOS, Linux, Windows) que lleva secretos de **AWS Secrets Manager** a tu proyecto, con dos modos:

- **Bruno**: declara las variables en un environment de la colección (`bruno/environments/<env>.bru`) como `CLAVE: {{process.env.CLAVE}}` y guarda los valores reales en `bruno/.env`.
- **.env**: rellena el `.env` del proyecto a partir de su plantilla (`.env.example`, `.env.template`…).

```
  secretsync  secretos de AWS Secrets Manager → Bruno / .env
  Modo Bruno  ›  Destino environment "dev" (bruno)  ›  Perfil dev  ›  Región eu-west-1

  ╭ Secretos  2/12 · 1 marcados ──────────────────────────────────────╮
  │  Filtro myapp                                                      │
  │   ● myapp/dev  Bruno dev                                           │
  │   ○ myapp/token                                                    │
  ╰────────────────────────────────────────────────────────────────────╯
  ↑↓ mover   espacio marcar   ctrl+a todos   enter continuar   esc cancelar
```

## Uso

Desde la raíz del repo:

```sh
secretsync            # menú interactivo
secretsync --dry-run  # igual, pero solo enseña lo que cambiaría
```

El flujo interactivo:

1. **¿Qué quieres hacer?** → Bruno o `.env`.
2. **Destino**
   - Bruno: busca `bruno/bruno.json` (también desde subcarpetas) y te deja elegir un environment o crear uno nuevo.
   - `.env`: usa `.env.example` / `.env.template` / `.env.sample` / `.env.dist` del directorio actual (si hay varias, pregunta).
3. **Perfil AWS**, leído de `~/.aws/credentials` y `~/.aws/config` (respeta `AWS_SHARED_CREDENTIALS_FILE` / `AWS_CONFIG_FILE`).
4. **Región**: `--region` → la del perfil → `AWS_REGION` / `AWS_DEFAULT_REGION` → selector.
5. **Secretos**: lista de la cuenta con filtro; marca uno o varios.
6. **Revisión**: diff por fichero (valores enmascarados) y botón *Escribir cambios*.
7. Si el `.env` no está ignorado por git, ofrece añadirlo al `.gitignore`.

Al salir, el resumen de lo que se ha hecho queda impreso en la terminal.

### Teclas

| Tecla | Acción |
|---|---|
| `↑` `↓` | moverse |
| escribir | filtrar la lista |
| `espacio` | marcar / desmarcar secreto |
| `ctrl+a` | marcar / desmarcar todos los visibles |
| `enter` | elegir / continuar |
| `←` `→` / `tab` | cambiar de botón |
| `esc` | borrar el filtro; si está vacío, cancelar |
| `ctrl+c` | cancelar en cualquier momento |

### Cómo se convierten los secretos

- Secreto JSON `{"CLAVE":"valor"}` → una variable por clave. Números y booleanos con su literal; objetos/arrays anidados como JSON compacto.
- Secreto en texto plano → una variable con el nombre del secreto normalizado (`my-app/db_pass` → `MY_APP_DB_PASS`).
- Si una clave aparece en varios secretos, gana el último elegido (y se avisa).

### Modo Bruno

- En el environment se escribe `CLAVE: {{process.env.CLAVE}}` dentro de `vars { }` (se crea el bloque o el fichero si no existen). El resto del fichero no se toca.
- El valor real va a `bruno/.env`, que es el único `.env` que lee Bruno y no debe versionarse.
- Las variables de `vars:secret [ ]` no se pueden rellenar desde fuera (Bruno las cifra localmente); si un secreto coincide con una de ellas se avisa y no se toca en el `.bru`.
- Si Bruno está abierto, recarga la colección para ver los cambios.

### Modo .env

- Solo se rellenan las claves que aparecen en la plantilla (el nombre se compara sin distinguir mayúsculas). Las claves de los secretos que no están en la plantilla se ignoran y se listan.
- Si `.env` no existe se crea copiando la plantilla (con sus comentarios) y sustituyendo los valores.
- Si existe, se actualizan las claves que vienen de los secretos, se añaden las que falten con el valor por defecto de la plantilla y el resto de líneas se conserva.
- Se avisa de las claves que se quedan sin valor.

En ambos modos la escritura es atómica y un `.env` nuevo se crea con permisos `0600`.

## Flags

| Flag | Descripción |
|---|---|
| `--mode bruno\|env` | salta el menú (útil en scripts) |
| `--profile` | perfil AWS |
| `--region` | región AWS |
| `--filter` | filtra la lista de secretos por nombre o descripción |
| `--secret` | secreto a inyectar; repetible. Omite la selección |
| `--environment` | [bruno] environment (se crea si no existe) |
| `--dir` | [bruno] ruta de la colección |
| `--template` | [env] plantilla |
| `--env-file` | [env] `.env` destino (por defecto junto a la plantilla) |
| `--dry-run` | muestra los cambios sin escribir |
| `--yes` | no pide confirmación (y añade el `.env` al `.gitignore` si hace falta) |
| `--version` | versión |

Sin terminal interactiva (CI, scripts) se usa salida de texto; hay que pasar todo por flags:

```sh
secretsync --mode bruno --environment dev --profile dev --secret myapp/dev --yes
secretsync --mode env --profile dev --secret myapp/dev --secret myapp/token --yes
```

## Instalación

**macOS / Linux con Homebrew**

```sh
brew install AVC99/tap/secretsync
brew upgrade secretsync   # actualizar
```

**macOS / Linux sin Homebrew**: descarga la última versión de GitHub Releases, comprueba el checksum y la deja en `/usr/local/bin` (o `~/.local/bin` si no tienes permiso). Para actualizar, vuelve a ejecutarlo.

```sh
curl -fsSL https://raw.githubusercontent.com/AVC99/secretsync/main/install.sh | sh
# versión o directorio concretos:
curl -fsSL https://raw.githubusercontent.com/AVC99/secretsync/main/install.sh | VERSION=v0.1.0 INSTALL_DIR=~/bin sh
```

**Con Go (cualquier plataforma, incluido Windows)**: requiere Go 1.26+.

```sh
go install github.com/AVC99/secretsync@latest
```

El binario queda en `$(go env GOPATH)/bin`; si no está en el PATH, en Windows añádelo desde PowerShell (y abre una terminal nueva):

```powershell
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';' + (go env GOPATH) + '\bin', 'User')
```

**Descarga manual**: en [Releases](https://github.com/AVC99/secretsync/releases) hay un `.tar.gz` (macOS/Linux) o `.zip` (Windows) por plataforma; descomprime y copia `secretsync` a un directorio del PATH. Los binarios son estáticos (`CGO_ENABLED=0`).

La UI (tview/tcell) funciona en Terminal/iTerm, terminales Linux y Windows Terminal/PowerShell.

### Desde el código

```sh
make install        # instala en $GOBIN / ~/go/bin y explica cómo añadirlo al PATH en cada plataforma
make build          # ./secretsync
make dist           # binarios en dist/ para darwin|linux|windows × amd64|arm64
make snapshot       # release completa en dist/ (archivos, checksums, cask) sin publicar; requiere goreleaser
```

## Desarrollo

```sh
make test
```

Para probar sin AWS real puedes apuntar el SDK a un endpoint local con `AWS_ENDPOINT_URL_SECRETS_MANAGER=http://127.0.0.1:4566` (LocalStack, por ejemplo).

## Flujo de trabajo y releases

Se trabaja en `dev` y se integra en `main` con un pull request:

1. Push a `dev` o PR a `main`/`dev` → workflow `ci`: `go vet` y `go test` en Linux, macOS y Windows, y un `goreleaser` en modo snapshot para comprobar que la release se puede generar.
2. Merge del PR en `main` → workflow `release`: vuelve a pasar los tests, calcula la versión siguiente, crea el tag y ejecuta GoReleaser, que publica la Release (binarios y `checksums.txt`) y actualiza el cask en [AVC99/homebrew-tap](https://github.com/AVC99/homebrew-tap).

La versión sale de las etiquetas del PR:

| Etiqueta | Ejemplo |
|---|---|
| (ninguna) | `v0.1.0` → `v0.1.1` |
| `minor` | `v0.1.0` → `v0.2.0` |
| `major` | `v0.1.0` → `v1.0.0` |
| `no-release` | no se publica nada |

La primera release es `v0.1.0`. El workflow necesita el secret `TAP_DEPLOY_KEY` en este repo: la clave privada de una deploy key con escritura en `homebrew-tap`.
