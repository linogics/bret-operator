# bret docs site

The docs are published at https://bret-operator.linogics.io.

This folder is an [Astro Starlight](https://starlight.astro.build) project. Pages live in
`src/content/docs/`. Diagrams are [D2](https://d2lang.com) code blocks and need the `d2` binary.

```bash
npm ci
npm run dev      # http://localhost:4321
npm run build    # writes dist/
```

`src/content/docs/reference/api.md` is generated. Run `make docs` from the repository root.

## Where the old pages went

| Old file | Page |
|---|---|
| `docs/README.md` | https://bret-operator.linogics.io/ |
| `docs/getting-started.md` | https://bret-operator.linogics.io/start/quickstart/ |
| `docs/installation.md` | https://bret-operator.linogics.io/start/installation/ |
| `docs/concepts.md` | https://bret-operator.linogics.io/concepts/overview/ |
| `docs/providers/README.md` | https://bret-operator.linogics.io/providers/ |
| `docs/providers/velero.md` | https://bret-operator.linogics.io/providers/velero/ |
| `docs/providers/volsync.md` | https://bret-operator.linogics.io/providers/volsync/ |
| `docs/providers/kopiur.md` | https://bret-operator.linogics.io/providers/kopiur/ |
| `docs/checks.md` | https://bret-operator.linogics.io/guides/write-a-check/ |
| `checks/README.md` | https://bret-operator.linogics.io/guides/check-catalog/ |
| `docs/testing-checks.md` | https://bret-operator.linogics.io/guides/test-a-check/ |
| `docs/restoretest-guide.md` | https://bret-operator.linogics.io/guides/restore-tests/ |
| `docs/data-and-database.md` | https://bret-operator.linogics.io/guides/data-and-database/ |
| `docs/best-practices.md` | https://bret-operator.linogics.io/guides/best-practices/ |
| `docs/troubleshooting.md` | https://bret-operator.linogics.io/reference/failure-reasons/ |
| `docs/reference/api.md` | https://bret-operator.linogics.io/reference/api/ |
| `docs/development.md` | https://bret-operator.linogics.io/contributing/development/ |
| `docs/releasing.md` | https://bret-operator.linogics.io/contributing/releasing/ |
