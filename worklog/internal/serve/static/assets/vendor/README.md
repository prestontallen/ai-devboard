# Vendored front-end runtime

These files are committed on purpose. `//go:embed` fails outright on an empty
directory, so a fresh clone with no npm present must already carry them for
`go build` to work — in CI, in Docker, and on `install.sh`'s local-build path.

Bytes are byte-identical to the published npm tarballs. Nothing is minified,
rewritten, or concatenated here; keeping them pristine is what makes a version
bump mechanical and drift detectable offline. `TestVendorChecksums` in
`serve_test.go` re-derives the hashes below.

Retrieved 2026-09-04 via `npm pack <pkg>@<version>`.

| File | Package | Version | License | sha256 |
|---|---|---|---|---|
| `preact.module.js` | [preact](https://www.npmjs.com/package/preact) (`dist/preact.module.js`) | 10.29.8 | MIT | `c30e721ebfdc6e2ad4c18c14d2dfb82667829c8aec27de1207774e3fc16858a8` |
| `preact.module.js.map` | preact | 10.29.8 | MIT | `23c9b5e6e405d2883bad8414d9483ad3ecdb85a94be52655db980cfc83460c29` |
| `hooks.module.js` | preact (`hooks/dist/hooks.module.js`) | 10.29.8 | MIT | `a6ee626f2d01570592dd569a792e3f050154aa02890eead8c223fa3ed5aa3d5a` |
| `hooks.module.js.map` | preact | 10.29.8 | MIT | `a13936803e904e19f2f154e953541c20dbbd0667c881f446e7aefafcfe487a97` |
| `htm.module.js` | [htm](https://www.npmjs.com/package/htm) (`dist/htm.module.js`) | 3.1.1 | Apache-2.0 | `ab33dd3f38059b9be4d5f5350128eefb2356639c4e0bbe9d9e8b3ba75847e9e4` |

The `.map` files ship so the module files can keep their upstream
`sourceMappingURL` trailers without emitting a console warning, and so the
Lens Board work has real stack traces.

## Why these exact artifacts

`hooks.module.js` opens with a bare `import{options as n}from"preact"`, so the
import map in `app.html` is load-bearing for the vendored files themselves,
not merely a convenience for our own code. Any change to the vendor filenames
must be mirrored there.

Preact is pinned to 10.x deliberately. 11.0.0-rc.1 exists; the redesign spike
called for waiting until 11 is *final*. `htm/preact/standalone` is
deliberately not used — it carries a frozen 2022 Preact inside.

Vendoring is a patch obligation (see CVE-2026-22028). To bump: `npm pack` the
new version, replace the files, update the table above, and run
`go test ./internal/serve/`.
