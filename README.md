# CLPS Lunar Mission Browser — Backend Integration Sandbox

Catatan: ini adalah sandbox integrasi peran backend untuk konsep CLPS Lunar Mission Browser. Container DS dan ML yang ada saat ini hanya stub untuk membuktikan alur koneksi; keduanya belum menjalankan analisis ilmiah, model, atau perhitungan misi.

## Gambaran alur

```text
Browser
  -> Frontend Nginx (:8080)
  -> Go backend (/api/run)
  -> DS service (/analyze)
  -> Go backend
  -> ML service (/predict)
  -> Go backend
  -> Frontend
```

Frontend hanya memanggil backend. Backend menjadi penghubung: memvalidasi request, mengirim input ke DS, meneruskan input dan hasil DS ke ML, lalu mengembalikan hasil keduanya dalam satu respons. DS dan ML tidak perlu dipanggil langsung oleh browser.

## Pembagian kerja

### Backend (Go)

- Menyediakan `POST /api/run` untuk frontend.
- Memvalidasi bentuk luar request: harus ada properti `input` berisi JSON yang valid.
- Mengirim `{"input": ...}` ke DS `POST /analyze`.
- Mengirim `{"input": ..., "ds_result": ...}` ke ML `POST /predict`.
- Mengembalikan `status`, `request_id`, `ds`, dan `ml` ke frontend.
- Menangani timeout dan kegagalan downstream.

Backend tidak menentukan algoritma DS/ML dan tidak mengubah isi hasil service. Objek hasil DS dan ML diteruskan sebagai JSON.

### Data Science (DS)

- Menyediakan HTTP service pada port internal `8001`.
- `GET /healthz` mengembalikan HTTP 2xx jika service siap.
- `POST /analyze` menerima objek `{"input": ...}` dan mengembalikan hasil DS dalam JSON.
- Tim DS menentukan dan mendokumentasikan isi hasil analisisnya.

### Machine Learning (ML)

- Menyediakan HTTP service pada port internal `8002`.
- `GET /healthz` mengembalikan HTTP 2xx jika service siap.
- `POST /predict` menerima objek `{"input": ..., "ds_result": ...}` dan mengembalikan hasil ML dalam JSON.
- Tim ML menentukan dan mendokumentasikan isi prediksinya.

### Frontend

- Tersedia di `http://localhost:8080` saat Compose berjalan.
- Mengirim `POST /api/run` dengan `Content-Type: application/json`.
- Menampilkan respons yang diterima dari backend.
- Frontend tidak memanggil DS atau ML secara langsung.

## Kontrak API backend

### Request dari frontend

```http
POST /api/run
Content-Type: application/json
```

```json
{
  "input": {
    "team_defined_fields": "replace with the field agreed by the team"
  }
}
```

`team_defined_fields` hanya contoh placeholder, bukan skema data CLPS yang sudah ditetapkan. Sebelum mengganti stub, tim harus menyepakati nama field, tipe data, satuan, dan aturan nilai untuk `input`.

### Request yang diterima DS

```json
{
  "input": {
    "...": "same input from frontend"
  }
}
```

### Request yang diterima ML

```json
{
  "input": {
    "...": "same input from frontend"
  },
  "ds_result": {
    "...": "complete JSON response from DS"
  }
}
```

### Response ke frontend

```json
{
  "status": "ok",
  "request_id": "unique-request-id",
  "ds": {
    "...": "DS response"
  },
  "ml": {
    "...": "ML response"
  }
}
```

`ds` dan `ml` berisi JSON response masing-masing service. Field di dalamnya ditentukan oleh tim DS dan ML; wrapper response di atas dimiliki backend.

Backend memberi HTTP 400 jika request frontend tidak valid dan HTTP 415 jika `Content-Type` bukan `application/json`. Body dibatasi 1 MiB dan harus berupa satu objek JSON dengan properti `input`. Jika DS atau ML gagal, timeout, mengembalikan status non-2xx, atau bukan JSON valid, backend mengembalikan error ke frontend.

Setiap request memperoleh `X-Request-ID`; ID yang sama dikirim ke DS dan ML dan dicatat di log JSON backend. Backend juga melakukan graceful shutdown saat container dihentikan. Timeout downstream, waktu shutdown, dan tingkat log dapat diatur lewat environment:

| Variable | Default | Kegunaan |
| --- | --- | --- |
| `REQUEST_TIMEOUT` | `20s` | Batas waktu gabungan pemanggilan DS dan ML |
| `SHUTDOWN_TIMEOUT` | `10s` | Batas waktu menunggu request selesai saat shutdown |
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, atau `ERROR` |

## Jaringan Docker

Compose menjalankan empat service: `frontend`, `backend`, `ds`, dan `ml`.

| Service | Alamat di dalam jaringan Compose | Bisa diakses dari host |
| --- | --- | --- |
| Frontend | `http://frontend:80` | `http://localhost:8080` |
| Backend | `http://backend:8080` | Tidak dipublikasikan |
| DS | `http://ds:8001` | Tidak dipublikasikan |
| ML | `http://ml:8002` | Tidak dipublikasikan |

Gunakan nama service (`ds`, `ml`, `backend`) untuk komunikasi antarkontainer. Jangan gunakan `localhost` untuk menghubungi container lain: di dalam container, `localhost` menunjuk ke container itu sendiri. Service DS dan ML harus listen di `0.0.0.0` agar dapat dijangkau dari container backend.

Compose menunggu health check DS dan ML sebelum memulai backend, lalu menunggu health check backend sebelum memulai frontend.

## Menjalankan sandbox

Butuh Docker Desktop dan Docker Compose v2. Dari folder proyek:

```powershell
docker compose up --build
```

Buka `http://localhost:8080`, isi JSON request, lalu pilih **Send through backend**. Hentikan dengan `Ctrl+C`, atau jalankan:

```powershell
docker compose down
```

Untuk melihat status container:

```powershell
docker compose ps
```

## Checklist sebelum mengganti stub

1. Sepakati skema `input` yang dibutuhkan frontend, DS, dan ML.
2. Sepakati bentuk JSON output DS dan ML, termasuk satuan dan arti tiap field.
3. Implementasikan kontrak route dan health check yang tercantum di README ini.
4. Pastikan service mendengarkan pada `0.0.0.0` dan menggunakan port internal yang sama (`8001` untuk DS, `8002` untuk ML).
5. Jalankan `docker compose up --build`, lalu coba request lewat frontend.

Data atau respons dari stub berlabel `stub_only` adalah data integrasi contoh, bukan pengukuran, prediksi, atau hasil sains CLPS. Jangan tampilkan sebagai hasil faktual.
