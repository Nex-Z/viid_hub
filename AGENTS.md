# Repository Guidelines

## Project Structure

- `cmd/viid/` contains the Go service. Device-facing `/VIID` handlers live in `protocol.go`, management queries in `query.go`, and image handling in `media.go`. Update `schema.sql` when PostgreSQL tables or views change.
- `web/src/` contains the React admin interface. Start in `records.jsx` for list behavior, `inspector.jsx` for details, and `overview.jsx` for dashboards. Reuse pagination and timestamp formatting from `shared.jsx` and shadcn/ui controls from `components/ui/`.
- `demo/media/` supplies sample images for the optional demo seed. Keep local credentials in the ignored `.env` file.

## Build and Local Development

Run commands from the repository root with Go 1.26.2+, Node.js 22.12+, and PostgreSQL. On first setup, copy `.env.example` to `.env` and configure `DATABASE_URL` and `VIID_DEVICE_PASSWORD`.

- `.\start.ps1` builds the frontend and serves the application at `127.0.0.1:14000`.
- For hot reload, run `go run ./cmd/viid` in one terminal. In another, run `npm --prefix web ci` followed by `npm --prefix web run dev`. Vite serves port 14001 and proxies both API paths to Go.
- `npm --prefix web run build` produces the frontend bundle in `web/dist/`.

## Style and Windows Tooling

Format Go changes with `gofmt`. Use two-space indentation in JSX and CSS, PascalCase for React components, and the `use` prefix for hooks such as `usePageSize`. Keep user-facing labels in Chinese.

On Windows, avoid `rg`. Search with `Get-ChildItem` and `Select-String`. Split large patches into smaller edits to stay within Windows command-length limits.

## Testing

Place Go tests in adjacent `*_test.go` files and name functions `TestXxx`. The standard-library `TestPostgresLifecycle` integration test creates and drops an isolated schema. It skips when its connection variable is unset. To exercise it against your configured development database:

```powershell
$env:VIID_TEST_DATABASE_URL = (Get-Content .env | ConvertFrom-StringData).DATABASE_URL
go test -count=1 -v ./...
go vet ./...
```

For UI changes, build the frontend and exercise the affected workflow in the browser.

## Commits and Pull Requests

Follow the history’s short, imperative subjects, such as `feat: default lists to 15 rows and remember page size`. In a PR, describe the behavior change and how you verified it. Link the related issue when applicable and include a screenshot for visible layout changes.
