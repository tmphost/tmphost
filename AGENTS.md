# tmphost Agent Guidelines

**General rules**
- 100% unit test coverage on both frontend and backend
- backend changes must follow [./backend/AGENTS.md](./backend/AGENTS.md)
- frontend changes must follow [./frontend/AGENTS.md](./frontend/AGENTS.md)
- every source file (Go, JS, Vue, CSS, HTML, shell, PowerShell, YAML, Dockerfile, Makefile) starts with the copyright notice (after a shebang or doctype); `make copyright-fix` adds it and CI fails without it
- `make lint` (golangci-lint on backend and cli, ESLint, copyright) must pass; CI runs it in the Lint and Copyright workflows
