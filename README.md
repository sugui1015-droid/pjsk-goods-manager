# PJSK Goods Manager

This repository now keeps the legacy Streamlit app and the new Vue 3 + Go stack side by side.

## Directory Layout

- `frontend/`: Vue 3 + TypeScript frontend.
- `backend/`: Go backend API.
- `legacy-streamlit/`: existing Streamlit version, kept runnable during migration.
- `docs/`: migration notes, database design, runbook, and workflow docs.
- `testdata/excel/`: local Excel parser test files and expected results.

## Run New Frontend

```bash
cd frontend
pnpm dev
```

Open `http://127.0.0.1:5173`.

## Run New Backend

```bash
cd backend
go run .
```

The backend listens on `http://127.0.0.1:8080` for local development and exposes:

- `GET /health`
- `GET /api/config`

## Run Legacy Streamlit

```bash
cd legacy-streamlit
python -m streamlit run main.py --server.port 8512
python -m streamlit run user.py --server.port 8513
```

The legacy app stays available until the new stack fully covers import, query, payment submission, and review.
