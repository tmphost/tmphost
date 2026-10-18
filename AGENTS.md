# tmphost Agent Guidelines

**General rules**
- 100% unit test coverage on both frontend and backend
- backend changes must follow [./backend/AGENTS.md](./backend/AGENTS.md)
- frontend changes must follow [./frontend/AGENTS.md](./frontend/AGENTS.md)
- every source file (.go, .js, .mjs, .vue, .css) starts with the copyright notice; `make copyright-fix` adds it and CI fails without it
- `make lint` (golangci-lint, ESLint, copyright) must pass; CI runs it in the Lint and Copyright workflows
