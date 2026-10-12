# Chirpy

Chirpy is a small Go HTTP API for posting and reading short messages called chirps. It uses PostgreSQL for users, chirps, and refresh tokens, and supports JWT access tokens, refresh-token revocation, and a Polka webhook for upgrading users to Chirpy Red.

## Requirements

- Go 1.26.8 or a compatible newer version (see `go.mod`)
- PostgreSQL
- [Goose](https://github.com/pressly/goose) for applying database migrations

Optional: install [sqlc](https://sqlc.dev/) if you need to regenerate the database package after editing SQL queries.

## Install and Configure

Clone the repository and download its Go dependencies:

```sh
git clone https://github.com/frankheinz87/chirpy.git
cd chirpy
go mod download
```

Create a PostgreSQL database named `chirpy`. For a local PostgreSQL installation with `createdb` available:

```sh
createdb chirpy
```

The database connection URL used below has no password in it:

```text
postgres://<your-postgres-user>@localhost:5432/chirpy?sslmode=disable
```

If your local PostgreSQL server requires a password, add it to your local `.env` connection URL only. Do not commit credentials.

Create a `.env` file in the repository root:

```dotenv
DB_URL=postgres://<your-postgres-user>@localhost:5432/chirpy?sslmode=disable
PLATFORM=dev
SECRET=replace-with-a-long-random-secret
POLKA_KEY=replace-with-your-polka-webhook-key
```

`DB_URL` is the PostgreSQL connection string. `SECRET` signs JWT access tokens. `POLKA_KEY` is used to authenticate Polka webhooks. `PLATFORM=dev` enables the destructive development reset endpoint; use a different value outside local development. `.env` is ignored by Git.

Install Goose if it is not already installed:

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Apply all migrations from the repository root. The commands below load `DB_URL` from `.env` without putting a password in the README or command history:

```sh
set -a
. ./.env
set +a
goose -dir sql/schema postgres "$DB_URL" up
```

Check migration status with:

```sh
goose -dir sql/schema postgres "$DB_URL" status
```

The migrations create users, chirps, hashed passwords, refresh tokens, and the `is_chirpy_red` flag. Chirps and refresh tokens reference users and are deleted when their user is deleted.

To regenerate Go database code after changing SQL files, install sqlc and run `sqlc generate` from the repository root:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
```

## Run

Start the API from the repository root:

```sh
go run .
```

The server listens on port `8080`. Verify it is running:

```sh
curl -i http://localhost:8080/api/healthz
```

Run the test suite with:

```sh
go test ./...
```

## HTTP API

JSON endpoints accept and return `application/json`. Error responses use this shape:

```json
{"error":"description"}
```

Protected user and chirp operations use `Authorization: Bearer <access-token>`. The refresh and revoke endpoints use the same header format with a refresh token in place of the access token. The Polka webhook instead uses `Authorization: Apikey <POLKA_KEY>`.

### Health, Static Files, and Admin

| Method and path | Purpose | Success |
| --- | --- | --- |
| `GET /api/healthz` | Health check; returns plain-text `OK` | `200` |
| `GET /app/` | Serves files from the repository directory | File server response |
| `GET /admin/metrics` | Displays the number of requests served through `/app/` | `200` HTML |
| `POST /admin/reset` | In `PLATFORM=dev` only, resets the file hit counter and deletes all users (and their related chirps and refresh tokens) | `200`; `403` unless in dev |

### Users and Authentication

| Method and path | Purpose | Authorization | Success |
| --- | --- | --- | --- |
| `POST /api/users` | Create a user | None | `201` |
| `POST /api/login` | Check credentials and issue access and refresh tokens | None | `200` |
| `PUT /api/users` | Change the authenticated user's email and password | Access token | `200` |
| `POST /api/refresh` | Exchange a valid refresh token for a new access token | Refresh token | `200` |
| `POST /api/revoke` | Revoke a refresh token | Refresh token | `204` |

Create a user:

```sh
curl -i -X POST http://localhost:8080/api/users \
	-H 'Content-Type: application/json' \
	-d '{"email":"reader@example.com","password":"correct-horse-battery-staple"}'
```

The request fields are `email` and `password`, both strings. The server hashes the password. Email must be unique in the database. A successful response includes the user's `id`, timestamps, `email`, and `is_chirpy_red`; token fields are empty until login.

Log in to receive tokens:

```sh
curl -i -X POST http://localhost:8080/api/login \
	-H 'Content-Type: application/json' \
	-d '{"email":"reader@example.com","password":"correct-horse-battery-staple"}'
```

A successful response includes `token` (a one-hour access JWT) and `refresh_token` (a refresh token valid for 60 days), as well as user fields. Invalid credentials return `401`.

Update the signed-in user's email and password. Both JSON values must be non-empty strings:

```sh
curl -i -X PUT http://localhost:8080/api/users \
	-H 'Authorization: Bearer <access-token>' \
	-H 'Content-Type: application/json' \
	-d '{"email":"new-address@example.com","password":"new-password"}'
```

Refresh an access token. This endpoint returns a new one-hour `token`; it does not rotate the refresh token:

```sh
curl -i -X POST http://localhost:8080/api/refresh \
	-H 'Authorization: Bearer <refresh-token>'
```

Revoke a refresh token:

```sh
curl -i -X POST http://localhost:8080/api/revoke \
	-H 'Authorization: Bearer <refresh-token>'
```

The revoke endpoint returns `204 No Content` on success.

### Chirps

| Method and path | Purpose | Authorization | Success |
| --- | --- | --- | --- |
| `GET /api/chirps` | List chirps, optionally filtered and sorted | None | `200` JSON array |
| `GET /api/chirps/{chirpID}` | Fetch one chirp by UUID | None | `200` |
| `POST /api/chirps` | Create a chirp for the authenticated user | Access token | `201` |
| `DELETE /api/chirps/{chirpID}` | Delete a chirp owned by the authenticated user | Access token | `204` |

`GET /api/chirps` accepts these query parameters:

| Parameter | Valid values | Behavior |
| --- | --- | --- |
| `author_id` | A user UUID | Return only chirps written by that user. Omit it to list chirps from all users. An invalid non-empty UUID returns `400`. |
| `sort` | `asc` or `desc` | Default is ascending creation time. `desc` returns newest first. Values other than `desc` currently use the default ascending order. |

Examples:

```sh
curl 'http://localhost:8080/api/chirps'
curl 'http://localhost:8080/api/chirps?author_id=<user-uuid>&sort=desc'
curl 'http://localhost:8080/api/chirps/<chirp-uuid>'
```

Create a chirp:

```sh
curl -i -X POST http://localhost:8080/api/chirps \
	-H 'Authorization: Bearer <access-token>' \
	-H 'Content-Type: application/json' \
	-d '{"body":"Hello from Chirpy!"}'
```

The required `body` is a string no longer than 140 bytes. The server associates the chirp with the user from the access token; any `user_id` sent in the body is not used. The words `kerfuffle`, `sharbert`, and `fornax` are replaced with `****` when they appear as space-separated words, without regard to letter case.

Delete a chirp you own:

```sh
curl -i -X DELETE http://localhost:8080/api/chirps/<chirp-uuid> \
	-H 'Authorization: Bearer <access-token>'
```

An invalid chirp UUID returns `400`, a missing chirp returns `404`, and attempting to delete another user's chirp returns `403`.

### Polka Webhook

`POST /api/polka/webhooks` accepts a Polka API key and a JSON event. The API key must match `POLKA_KEY`:

```sh
curl -i -X POST http://localhost:8080/api/polka/webhooks \
	-H 'Authorization: Apikey <polka-key>' \
	-H 'Content-Type: application/json' \
	-d '{"event":"user.upgraded","data":{"user_id":"<user-uuid>"}}'
```

The `user.upgraded` event sets that user's `is_chirpy_red` field to `true` and returns `204 No Content`. Other event names are ignored and also return `204`. Missing or incorrect API keys return `401`; malformed JSON returns `400`; an unknown user on an upgrade event returns `404`.