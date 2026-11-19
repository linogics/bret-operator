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
