# fileupload

Minimal Drive clone — backend only. Go `net/http` + `pgx` + Postgres + Supabase Storage + Google OAuth. Frontend hasn't been built yet, so you test it with Bruno/curl.

No frameworks, no ORM. `net/http` 1.22 mux with `METHOD /path` patterns, raw SQL, `pgx` stdlib driver.

## Stack

- Go 1.27
- `jackc/pgx/v5` (stdlib) + `database/sql`
- `golang-jwt/jwt/v5` — 7-day `session` cookie, `HttpOnly`, `SameSite=Lax`
- `golang.org/x/oauth2` — Google OAuth2
- Supabase Storage (S3-compatible, used via REST `storage/v1/object`)
- Postgres 16 (local or Neon)

## Layout

```
cmd/api          -> http server, route wiring
cmd/migrate      -> tiny migrator, reads migrations/*.up.sql + schema_migrations table
internal/config  -> env loading + validation
internal/db      -> Open() with pool 25/5/30m, Ping
internal/auth    -> GenerateState, FetchGoogleUser, IssueSessionToken, RequireAuth middleware
internal/handlers-> auth, folders, files
internal/models  -> User/Folder/File structs
internal/storage -> Storage interface + SupabaseStorage
internal/middleware -> (placeholder)
migrations/      -> 000001_create_core_tables.{up,down}.sql
Taskfile.yml     -> task run / task migrate / task dev
```

## Env

Copy `.env.example` to `.env`:

```
PORT=8080
ENV=development
DATABASE_URL=postgres://postgres:admin@localhost:5432/fileupload?sslmode=disable
JWT_SECRET=<32+ chars, e.g. openssl rand -base64 32>
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GOOGLE_REDIRECT_URL=http://localhost:8080/auth/google/callback
SUPABASE_URL=https://<project>.supabase.co
SUPABASE_SECRET_KEY=sb_secret_...
SUPABASE_BUCKET=files
FRONTEND_URL=http://localhost:3000
```

Google Cloud: `APIs & Services -> Credentials -> Create OAuth client (Web)`. Add `http://localhost:8080/auth/google/callback` to authorized redirects. Supabase: project -> Storage -> new bucket `files` (public off) -> `API -> secret key`.

`JWT_SECRET` must be >=32 chars or `config.Load()` fails. `PORT` validated 1-65535.

## Running

Requires Go + Task (`go install github.com/go-task/task/v3/cmd/task@latest`) + Postgres.

```sh
cp .env.example .env   # fill it
task migrate           # go run ./cmd/migrate up -> reads migrations/*.up.sql
task dev               # migrate + run
# or manually:
task run               # go run ./cmd/api (Task loads .env via dotenv)
go vet ./... && go test ./...
```

`task migrate` expects `migrations/*.up.sql` at repo root. The file `internal/migrations` is not used — root `migrations/` is the source of truth.

Server: `http://localhost:8080`

- `GET /healthz` -> `ok`
- `GET /auth/google/login` -> 302 to Google
- `GET /auth/google/callback` -> upserts `users` (`google_id` ON CONFLICT), sets `session` cookie, 302 to `FRONTEND_URL`

If you run `go run ./cmd/api` directly without Task, `DATABASE_URL is required` — use Task or export env manually.

## Auth

Protected routes check `Cookie: session` via `RequireAuth` (`internal/auth/middleware.go`). Token is HS256, `uid` claim, 7d TTL (`internal/auth/jwt.go`).

Browser login is the easiest way to get a cookie locally. After login, open DevTools -> Application -> Cookies -> `localhost:8080` -> copy `session`. In Bruno/Postman add header `Cookie: session=<jwt>` to every protected request. The cookie is `HttpOnly`, so `document.cookie` won't show it.

If you don't want to click through Google, generate a dev token:

```sh
# get a user id
psql $DATABASE_URL -c "insert into users (id, google_id, email, name) values (gen_random_uuid(), 'dev', 'dev@test.com', 'Dev') returning id"
# gen.go
cat > /tmp/gen.go <<'Go'
package main
import ("fmt"; "os"; "github.com/Falasefemi2/fileupload/internal/auth")
func main(){ t,_ := auth.IssueSessionToken(os.Getenv("JWT_SECRET"), "<id>"); fmt.Println(t) }
Go
JWT_SECRET=$(grep JWT_SECRET .env | cut -d= -f2) go run /tmp/gen.go
```

## API

All JSON, all protected except `/auth/google/*` and `/healthz`.

```
POST   /folders                      {"name":"Docs","parentId":"uuid|null"} -> 201 Folder
GET    /folders/contents             # root
GET    /folders/{id}/contents        -> {folders:[], files:[]}
DELETE /folders/{id}                 # cascades to subfolders+files, best-effort Supabase delete

POST   /files/init                   {"name":"a.txt","mimeType":"text/plain","folderId":"uuid|null","size":123} -> 201 {fileId, storageKey}
POST   /files/{id}/complete          # verifies object exists in Supabase via Download, sets status=confirmed
GET    /files/{id}/download          # streams bytes, Content-Type + Content-Disposition
DELETE /files/{id}                   # db delete + Supabase Delete
```

Folder `parent_id` validated to belong to you. `ListContents` uses `r.PathValue("id")` (Go 1.22). File keys are `<userID>/<fileId>` — `storageKey` returned by `init` is the Supabase object key.

Example with curl (after login, `$TOKEN` is session value):

```sh
curl -H "Cookie: session=$TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"My Folder"}' http://localhost:8080/folders

curl -H "Cookie: session=$TOKEN" http://localhost:8080/folders/contents

curl -H "Cookie: session=$TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"hello.txt","mimeType":"text/plain"}' http://localhost:8080/files/init
# -> {"fileId":"...","storageKey":"<uid>/..."}

# upload bytes to Supabase (separate from Go server)
curl -X POST "https://<project>.supabase.co/storage/v1/object/files/<uid>/<fileId>" \
  -H "apikey: $SUPABASE_SECRET_KEY" -H "Authorization: Bearer $SUPABASE_SECRET_KEY" \
  -H "Content-Type: text/plain" --data-binary "hello world"

curl -X POST -H "Cookie: session=$TOKEN" http://localhost:8080/files/<fileId>/complete
curl -H "Cookie: session=$TOKEN" http://localhost:8080/files/<fileId>/download --output out.txt
```

Bruno: set `{{baseUrl}}` + `{{session}}` env vars, add `headers { Cookie: session={{session}} }` at collection level so it inherits.

## Storage

Was originally R2 presigned URLs (see git history), now Supabase Storage. Same flow: server never proxies file bytes except on `download` (it streams from Supabase). `init` just creates a `files` row `status=pending`, `complete` does a `Download` check before marking `confirmed` — don't trust client. `SupabaseStorage` (`internal/storage/supabase.go`) does `POST /storage/v1/object/{bucket}/{key}` for upload, `GET` for download, `DELETE` for delete, with `apikey` + `Bearer` headers. No presigning — if you need it, add `createSignedUrl` instead of streaming.

## Testing

On branch `test-branch`:

```sh
go test ./internal/config -v   # Load, defaults, required keys, JWT length, getEnv trimming, port validation
go test ./internal/db -v       # Open empty/invalid/unreachable/cancelled + integration when DATABASE_URL set
go test ./internal/auth -v     # GenerateState uniqueness, NewGoogleAuth, Issue/Parse, RequireAuth, UserIDFromContext
go test ./internal/storage -v  # NewSupabaseStorage trim, Upload/Download/Delete success + 400/404/500 + context cancelled + transport error (httptest)
go test ./...                  # all
```

No mocks beyond `httptest` + `sql` error paths. DB integration test skips if `DATABASE_URL` not set.

## Notes

- `users.id`, `folders.id`, `files.id` are `UUID` generated in Go (`google/uuid`), not `DEFAULT gen_random_uuid()` — migration `000001_create_core_tables.up.sql` has no default.
- `folders.parent_id` and `files.folder_id` are nullable; `ON DELETE CASCADE` so deleting a folder cascades. `DeleteFolder` also collects `storage_key`s via `WITH RECURSIVE` and deletes from Supabase best-effort.
- No `deleted_at` soft delete, no reaper job — hard delete. Add a `deleted_at` column + cron if you want the old R2 soft-delete design back.
- No rename/move yet. It's `UPDATE folders SET name=$1` / `UPDATE files SET folder_id=$1` with ownership check.
- `FRONTEND_URL` only used for post-login redirect. CORS is not configured — add middleware when you have a frontend.

## Next

- `PATCH /folders/{id}` + `PATCH /files/{id}/move`
- Signed download URLs instead of streaming through Go
- `MaxBytesReader` on JSON bodies
- `migrations` tooling with `down` support already works (`internal/migrations/*.down.sql` copied to root)
