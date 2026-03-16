# AI Generator API

## Base URL

`http://localhost:8080`

## Standard JSON Response

### Success

```json
{
  "status": "success",
  "message": "generation completed",
  "data": {}
}
```

### Error

```json
{
  "status": "error",
  "message": "invalid request body",
  "error": "prompt must not be empty"
}
```

## Endpoint: Generate

- Method: `POST`
- URL: `/api/v1/generate`
- Content-Type: `application/json`

### Request Body

```json
{
  "prompt": "floating glowing crystal core above reflective dark surface, soft cyan and violet light threads inside crystal, subtle energy particles drifting, cinematic volumetric lighting, minimal mystical technology aesthetic, ultra detailed, 8k",
  "file": "image",
  "auto_metadata": true,
  "enhance": true,
  "generate": 5,
  "debug": false,
  "auto_prompt": false,
  "strict_single_object": false,`n  "variation_strength": "medium",`n  "aspect_ratio": "16:9",
  "model_id": "2067ae52-33fd-4a82-bb92-c2c55e7d2786",
  "style_uuid": "111dc692-d470-4eec-b791-3475abac4c46",
  "seed": 12345
}
```

Notes:
- `prompt` wajib diisi dan menjadi sumber utama prompt Leonardo.
- `file` wajib: `image`, `video`, atau `vector`.
- `auto_metadata=true` akan generate `adobe_stock_metadata.csv` via ChatGPT untuk semua file hasil.
- `generate` adalah jumlah target file yang akan dibuat sesuai mode `file`.
- `file=vector` akan output `.svg` (auto-vectorized).
- `enhance=true` mengaktifkan enhance image, dan video memakai high-quality enhancement cues.
- `auto_prompt=true` akan minta OpenAI riset prompt potensial minggu depan berdasarkan konteks seasonal/international day bulan berjalan.
- `auto_prompt=false` mengaktifkan `prompt passthrough mode` (prompt manual tidak diubah agar hasil lebih mirip Leonardo Web).
- `strict_single_object=true` memaksa prompt menjadi satu objek utama saja (anti cluster/duplicate/fragment).`n- `variation_strength` opsional: `low`, `medium`, `high` untuk level perbedaan antar prompt batch.
- `aspect_ratio` opsional: `16:9`, `1:1`, `4:5`, `9:16`, `3:2`.
- `model_id` wajib jika `LEONARDO_REQUIRE_MODEL_ID=true` (default), override model Leonardo per-request.
- `style_uuid` opsional: override style Leonardo per-request.
- `seed` opsional: untuk hasil lebih konsisten antar percobaan.
- Jika OpenAI gagal (misalnya quota limit/429), service fallback otomatis ke file lokal `data/prompt_catalog.json`.
- Jika `debug=true`, service mode hemat cost:
  - `generate=1`
  - `enhance=true`
  - mode `file` tetap mengikuti request

### Success Response

```json
{
  "status": "success",
  "message": "generation completed",
  "data": {
    "job_id": "job_83492311aa22",
    "file_type": "image",
    "total_prompt": 2,
    "total_image": 8,
    "total_file": 8,
    "zip_file": "/download/job_83492311aa22"
  }
}
```

### Error Response

```json
{
  "status": "error",
  "message": "failed to generate files",
  "error": "leonardo call failed after retries: http 401: unauthorized"
}
```

## Endpoint: Download ZIP

- Method: `GET`
- URL: `/download/{job_id}`
- Response: File download (`application/zip`)

Optional query:
- `cleanup=1` atau `cleanup=true` untuk menghapus folder job di `storage` setelah file berhasil ditransfer.

Example:

`GET /download/job_83492311aa22`
`GET /download/job_83492311aa22?cleanup=1`

## Environment Variables

- `PORT` default `8080`
- `STORAGE_PATH` default `storage`
- `LEONARDO_API_KEY` required when `LEONARDO_MOCK=false`
- `LEONARDO_API_KEYS` optional comma-separated multiple keys for auto failover
- `LEONARDO_BASE_URL` default `https://cloud.leonardo.ai/api/rest/v1`
- `LEONARDO_GENERATE_ENDPOINT` default `/generations`
- `LEONARDO_UPSCALE_ENDPOINT` default `/variations/upscale`
- `LEONARDO_VIDEO_ENDPOINT` default `/generations-text-to-video`
- `LEONARDO_STYLE_UUID` optional, gunakan jika ingin style sama seperti Leonardo manual UI
- `LEONARDO_MODEL_ID` disarankan wajib, gunakan model yang sama dengan Leonardo manual UI (mis. Concept Art)
- `LEONARDO_DEFAULT_ALCHEMY` default `false` (set `true` jika ingin default alchemy aktif)
- `LEONARDO_REQUIRE_MODEL_ID` default `true` untuk mencegah fallback model default
- `HTTP_TIMEOUT_SECONDS` default `60`
- `RETRY_COUNT` default `3`
- `RETRY_DELAY_MS` default `1200`
- `MAX_IMAGE_WORKERS` default `5`
- `MAX_ENHANCE_WORKERS` default `5`
- `MAX_VIDEO_WORKERS` default `2`
- `VIDEO_QUEUE_SIZE` default `200`
- `VARIANT_MIN` default `3`
- `VARIANT_MAX` default `5`
- `MAX_IMAGES_PER_PROMPT` default `10`
- `MAX_TOTAL_IMAGES` default `500`
- `PROMPT_CATALOG_PATH` default `data/prompt_catalog.json`
- `LEONARDO_MOCK` default `false`
- `OPENAI_API_KEY` required when using `niche` mode
- `OPENAI_BASE_URL` default `https://api.openai.com/v1`
- `OPENAI_MODEL` default `gpt-4o-mini`
- `NICHE_PROMPT_COUNT` default `8`

## Run

```bash
go mod tidy
go run .
```
