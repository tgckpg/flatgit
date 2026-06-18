# flatgit

`flatgit` is a static Git web generator with webhook-driven updates and an optional built-in file server.

## Philosophy

Git repo in, static browsable site out.

```txt
webhook -> git clone/fetch --mirror -> render HTML + JSON -> serve static files
```

It currently shells out to the real `git` binary.

Since Git already knows how to deal with packed refs, tags, bare repositories, weird histories, and future compatibility.
The main focus is on rendering.

HTML should be served by proper http server if possible. The built-in server is fine but basic.

## Current status

- `flatgit render` one-shot renderer
- `flatgit serve` static file server
- `flatgit daemon` webhook + render workers + static serving
- mirror clone/fetch using `git clone --mirror` and `git remote update --prune`
- simple HTML pages:
  - summary
  - refs
  - log
  - tree listing
  - blob view
  - commit patch view
- JSON files:
  - `flatgit.json`
  - `refs.json`
  - `commits.json`
  - `tree.json`
  - `commit/<sha>.json`
- Gitea/GitHub-style webhook endpoints:
  - `POST /webhook/gitea`
  - `POST /webhook/github`

## Build

```sh
make clean build
```

## Config

See [`examples/tinyproxy.json`](examples/tinyproxy.json):

```json
{
  "addr": ":8080",
  "data_dir": "/var/lib/flatgit",
  "webhook": {
    "secret": "change-me"
  },
  "git": {
    "command": "git",
    "clone_timeout": "2m",
    "fetch_timeout": "2m"
  },
  "render": {
    "workers": 2,
    "max_commits": 500
  },
  "repos": [
    {
      "owner": "penguin",
      "name": "test-repo",
      "url": "http://gitea-http.gitea.svc.cluster.local:3000/penguin/test-repo.git",
      "default_branch": "main"
    }
  ]
}
```

Derived paths:

```txt
/var/lib/flatgit/repos/penguin_test-repo.git
/var/lib/flatgit/www/penguin_test-repo
```

## Commands

Render all configured repos:

```sh
flatgit render -c examples/flatgit.json
```

Render one repo:

```sh
flatgit render -c examples/flatgit.json -repo penguin_test-repo
```

Serve an already-rendered web root:

```sh
flatgit serve -root /var/lib/flatgit/www -addr :8080
```

Run the daemon:

```sh
flatgit daemon -c examples/flatgit.json
```

The daemon serves:

```txt
/                       static web root
/healthz                health check
/webhook/gitea          Gitea webhook endpoint
/webhook/github         GitHub webhook endpoint
```

## Webhook signature

If `webhook.secret` is non-empty, the handler accepts either:

- `X-Hub-Signature-256: sha256=<hex-hmac>`
- `X-Gitea-Signature: <hex-hmac>`

The HMAC is SHA-256 over the raw request body.

## TODO

- Add README rendering
- Add syntax highlighting, probably with a tiny vendored/highlight-free first pass
- Improve branch/tag URL escaping
- Render per-branch trees instead of only the default branch
- Add archive links
- Add repo index page at the web-root
- Add tests using temporary local Git repos
