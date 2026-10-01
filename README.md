# Backend Integration Sandbox

Go backend + Docker Compose + frontend integration scaffold. The DS and ML folders contain only contract stubs so teammates can plug in their own services; they contain no algorithms or challenge-specific data.

## Start
Requires Docker Desktop with Compose v2. From this folder run:

    docker compose up --build

Open http://localhost:8080 and press "Send through backend". Stop with Ctrl+C.

## Flow
Browser -> Nginx frontend -> Go API -> DS -> Go API -> ML -> Go API -> frontend

Only frontend port 8080 is published. Backend, DS, and ML communicate over the private Compose network using service DNS names.

## Contract
Frontend sends POST /api/run:

    {"input": {"team_defined_fields": "..." }}

Backend sends {"input": ...} to DS POST /analyze. It then sends {"input": ..., "ds_result": ...} to ML POST /predict. Backend returns a stable outer envelope with request_id, ds, and ml. Service output objects remain opaque to Go.

Backend owns input validation, timeouts, orchestration, downstream errors, and the outer response. DS owns its analysis and output details. ML owns its model and output details. Frontend owns presentation and calls only the Go API.

Replace the DS and ML stub implementations with teammate services while preserving service names, ports, and routes. Agree the inner JSON fields together before integration. Stub responses are not scientific results.
