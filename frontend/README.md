# Vue 3 Frontend

Minimal Vue 3 + TypeScript shell for the new PJSK frontend.

## Run

```bash
pnpm dev
```

During local development, the frontend uses relative `/api` and `/health` requests. Vite proxies them to the Go backend on `http://127.0.0.1:8080`.

`VITE_API_BASE_URL` is only used for non-development builds.
