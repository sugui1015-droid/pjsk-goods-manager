# Backend

Minimal Go API scaffold for the Vue 3 migration.

## Run

```bash
go run .
```

## Health check

```bash
curl http://127.0.0.1:8080/health
```

## Administrator authentication

Create the first administrator interactively; the password is read without terminal echo:

```bash
go run ./cmd/create-admin -username admin
```

The API exposes `POST /api/admin/login`, `GET /api/admin/me`, and `POST /api/admin/logout`. Sessions use an HttpOnly cookie, while PostgreSQL stores only the token hash.
