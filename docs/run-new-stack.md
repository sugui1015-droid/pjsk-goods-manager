# Run New Stack

## Quick Start

From `D:\pjsk`:

```bash
backend\run.cmd
frontend\run.cmd
```

Open `http://127.0.0.1:5173`.

## Backend

In `D:\pjsk\backend`:

```bash
go run .
```

If a new terminal does not recognize `go` yet, reopen the terminal first. The Go binary is installed at `D:\go\bin\go.exe`, and `backend\run.cmd` already adds it to PATH for that process.

The backend listens on `http://127.0.0.1:8080` for local development.

Available endpoints:

- `GET /health`
- `GET /api/config`

## Frontend

In `D:\pjsk\frontend`:

```bash
pnpm dev
```

The frontend runs on `http://127.0.0.1:5173`.
In dev mode, the browser uses relative `/health` and `/api` requests, and Vite proxies them to `http://127.0.0.1:8080`.
If the backend is not running yet, the frontend still opens in local shell mode.

## Legacy Streamlit

The old Streamlit app stays online during migration:

```bash
cd D:\pjsk\legacy-streamlit
python -m streamlit run main.py --server.port 8512
python -m streamlit run user.py --server.port 8513
```
